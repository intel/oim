/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"fmt"
	"os"

	"golang.org/x/net/context"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/kubernetes/pkg/util/mount"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/spdk"
)

type nodeServer struct {
	*DefaultNodeServer
}

func findNBDDevice(ctx context.Context, client *spdk.Client, volumeID string) (nbdDevice string, err error) {
	nbdDisks, err := spdk.GetNBDDisks(ctx, client)
	if err != nil {
		return "", status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to get NDB disks from SPDK: %s", err))
	}
	for _, nbd := range nbdDisks {
		if nbd.BDevName == volumeID {
			return nbd.NBDDevice, nil
		}
	}
	return "", nil
}

func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {

	// Check arguments
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability missing in request")
	}
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	// Check and prepare mount point.
	targetPath := req.GetTargetPath()
	notMnt, err := mount.New("").IsLikelyNotMountPoint(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err = os.MkdirAll(targetPath, 0750); err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			notMnt = true
		} else {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	if !notMnt {
		// Already mounted, nothing to do.
		return &csi.NodePublishVolumeResponse{}, nil
	}

	fsType := req.GetVolumeCapability().GetMount().GetFsType()
	readOnly := req.GetReadonly()
	volumeID := req.GetVolumeId()
	attrib := req.GetVolumeAttributes()
	mountFlags := req.GetVolumeCapability().GetMount().GetMountFlags()

	oimcommon.Infof(4, ctx, "target %v\nfstype %v\nreadonly %v\nattributes %v\n mountflags %v\n",
		targetPath, fsType, readOnly, volumeID, attrib, mountFlags)

	// Connect to SPDK.
	// TODO: make this configurable and decide whether we need to
	// talk to a local or remote SPDK.
	client, err := spdk.New("/var/tmp/spdk.sock")
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to connect to SPDK: %s", err))
	}
	defer client.Close()

	// We might have already mapped that BDev to a NBD disk - check!
	nbdDevice, err := findNBDDevice(ctx, client, volumeID)
	if err != nil {
		return nil, err
	}
	if nbdDevice != "" {
		oimcommon.Infof(4, ctx, "Reusing already started NBD disk: %s", nbdDevice)
	} else {
		var nbdError error
		// Find a free NBD device node and start a NBD disk there.
		// Unfortunately this is racy. We assume that we are the
		// only users of /dev/nbd*.
		for i := 0; ; i++ {
			n := fmt.Sprintf("/dev/nbd%d", i)
			nbdFile, err := os.Open(n)
			// We stop when we run into the first non-existent device name.
			if os.IsNotExist(err) {
				if nbdError == nil {
					nbdError = err
				}
				break
			}
			if err != nil {
				nbdError = err
				continue
			}
			defer nbdFile.Close()
			size, err := oimcommon.GetBlkSize64(nbdFile)
			nbdFile.Close()
			if err != nil {
				nbdError = err
				continue
			}
			if size == 0 {
				// Seems unused, take it.
				nbdDevice = n
				break
			}
		}
		// Still nothing?!
		if nbdDevice == "" {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to find an unused /dev/nbd*: %s", nbdError))
		}
	}

	args := spdk.StartNBDDiskArgs{
		BDevName:  volumeID,
		NBDDevice: nbdDevice,
	}
	if err := spdk.StartNBDDisk(ctx, client, args); err != nil {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to start SPDK NBD disk %+v: %s", args, err))
	}

	options := []string{}
	if readOnly {
		options = append(options, "ro")
	}
	diskMounter := &mount.SafeFormatAndMount{Interface: mount.New(""), Exec: mount.NewOsExec()}
	if err := diskMounter.FormatAndMount(nbdDevice, targetPath, fsType, options); err != nil {
		return nil, err
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}
	targetPath := req.GetTargetPath()
	volumeID := req.GetVolumeId()

	// Connect to SPDK.
	// TODO: make this configurable and decide whether we need to
	// talk to a local or remote SPDK.
	client, err := spdk.New("/var/tmp/spdk.sock")
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to connect to SPDK: %s", err))
	}
	defer client.Close()

	// Unmounting the image
	// TODO: check whether this really is still a mount point. We might have removed it already.
	if err := mount.New("").Unmount(req.GetTargetPath()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	oimcommon.Infof(4, ctx, "volume %s/%s has been unmounted.", targetPath, volumeID)

	// Stop NBD disk.
	nbdDevice, err := findNBDDevice(ctx, client, volumeID)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to get NDB disks from SPDK: %s", err))
	}
	args := spdk.StopNBDDiskArgs{NBDDevice: nbdDevice}
	if err := spdk.StopNBDDisk(ctx, client, args); err != nil {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to stop SPDK NDB disk %+v: %s", args, err))
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetStagingTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetStagingTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}
