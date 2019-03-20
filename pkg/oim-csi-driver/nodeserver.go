/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"context"
	"os"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/mount"
)

func (od *oimDriver) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId: od.nodeID,
	}, nil
}

func (od *oimDriver) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
		},
	}, nil
}

func (od *oimDriver) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	targetPath := req.GetTargetPath()
	stagingTargetPath := req.GetStagingTargetPath()
	volumeID := req.GetVolumeId()
	volumeCapability := req.GetVolumeCapability()
	readOnly := req.GetReadonly()

	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "empty target path")
	}
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "empty staging target path")
	}
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "empty volume ID")
	}
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "missing volume capability")
	}

	// Volume ID is the same as the volume name in CreateVolume. Serialize by that.
	volumeNameMutex.LockKey(volumeID)
	defer volumeNameMutex.UnlockKey(volumeID)

	mounter := mount.New("")

	// The following code was copied from:
	// https://github.com/kubernetes-sigs/gcp-compute-persistent-disk-csi-driver/blob/fa02b8971cb686b3e2e9cd965c18fde3ccadd10a/pkg/gce-pd-csi-driver/node.go#L45

	notMnt, err := mounter.IsLikelyNotMountPoint(targetPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, status.Error(codes.Internal, errors.Wrap(err, "validate target path").Error())
	}
	if !notMnt {
		// TODO(https://github.com/kubernetes-sigs/gcp-compute-persistent-disk-csi-driver/issues/95): check if mount is compatible. Return OK if it is, or appropriate error.
		/*
			1) Target Path MUST be the vol referenced by vol ID
			2) VolumeCapability MUST match
			3) Readonly MUST match
		*/
		return &csi.NodePublishVolumeResponse{}, nil
	}

	if err := mounter.MakeDir(targetPath); err != nil {
		return nil, status.Error(codes.Internal, errors.Wrap(err, "make target dir").Error())
	}

	// Perform a bind mount to the full path to allow duplicate mounts of the same PD.
	options := []string{"bind"}
	if readOnly {
		options = append(options, "ro")
	}

	err = mounter.Mount(stagingTargetPath, targetPath, "ext4", options)
	if err != nil {
		notMnt, mntErr := mounter.IsLikelyNotMountPoint(targetPath)
		if mntErr != nil {
			return nil, status.Error(codes.Internal, errors.Wrap(mntErr, "check whether target path is a mount point").Error())
		}
		if !notMnt {
			if mntErr = mounter.Unmount(targetPath); mntErr != nil {
				return nil, status.Error(codes.Internal, errors.Wrap(mntErr, "unmount target path").Error())
			}
			notMnt, mntErr := mounter.IsLikelyNotMountPoint(targetPath)
			if mntErr != nil {
				return nil, status.Error(codes.Internal, errors.Wrap(mntErr, "check whether target path is a mount point").Error())
			}
			if !notMnt {
				// This is very odd, we don't expect it.  We'll try again next sync loop.
				return nil, status.Error(codes.Internal, errors.Wrap(mntErr, "something is wrong with mounting").Error())
			}
		}
		os.Remove(targetPath) // nolint: gosec
		return nil, status.Error(codes.Internal, errors.Wrap(err, "mount of disk failed").Error())
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (od *oimDriver) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	targetPath := req.GetTargetPath()
	volumeID := req.GetVolumeId()

	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "empty target path")
	}
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "empty volume ID")
	}

	// Volume ID is the same as the volume name in CreateVolume. Serialize by that.
	volumeNameMutex.LockKey(volumeID)
	defer volumeNameMutex.UnlockKey(volumeID)

	// The following code was copied from:
	// https://github.com/kubernetes-sigs/gcp-compute-persistent-disk-csi-driver/blob/master/pkg/gce-pd-csi-driver/node.go#L128

	mounter := mount.New("")
	err := mount.UnmountPath(targetPath, mounter)
	if err != nil {
		return nil, status.Error(codes.Internal, errors.Wrap(err, "unmount failed").Error())
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (od *oimDriver) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	targetPath := req.GetStagingTargetPath()
	volumeID := req.GetVolumeId()
	volumeCapability := req.GetVolumeCapability()

	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "empty target path")
	}
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "empty volume ID")
	}
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "missing volume capability")
	}

	// Volume ID is the same as the volume name in CreateVolume. Serialize by that.
	volumeNameMutex.LockKey(volumeID)
	defer volumeNameMutex.UnlockKey(volumeID)

	// Check and prepare mount point.
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
		return &csi.NodeStageVolumeResponse{}, nil
	}

	fsType := req.GetVolumeCapability().GetMount().GetFsType()
	mountFlags := req.GetVolumeCapability().GetMount().GetMountFlags()

	log.FromContext(ctx).Infow("mounting",
		"target", targetPath,
		"fstype", fsType,
		"volumeid", volumeID,
		"flags", mountFlags,
	)

	device, cleanup, err := od.backend.createDevice(ctx, volumeID, req)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	options := []string{}
	diskMounter := &mount.SafeFormatAndMount{Interface: mount.New(""), Exec: mount.NewOsExec()}
	if err := diskMounter.FormatAndMount(device, targetPath, fsType, options); err != nil {
		// We get a pretty bad error code from FormatAndMount ("exit code 1") :-/
		return nil, status.Error(codes.Internal, errors.Wrapf(err, "formatting as %s and mounting %s at %s", fsType, device, targetPath).Error())
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (od *oimDriver) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	targetPath := req.GetStagingTargetPath()
	volumeID := req.GetVolumeId()

	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "empty target path")
	}
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "empty volume ID")
	}

	// Volume ID is the same as the volume name in CreateVolume. Serialize by that.
	volumeNameMutex.LockKey(volumeID)
	defer volumeNameMutex.UnlockKey(volumeID)

	// Unmounting the image
	// TODO: check whether this really is still a mount point. We might have removed it already.
	log.FromContext(ctx).Infow("unmount",
		"target", targetPath,
		"volumeid", volumeID,
	)
	if err := mount.New("").Unmount(targetPath); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if err := od.backend.deleteDevice(ctx, volumeID); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (od *oimDriver) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	volumeID := req.GetVolumeId()
	volumePath := req.GetVolumePath()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "empty volume ID")
	}
	if volumePath == "" {
		return nil, status.Error(codes.InvalidArgument, "empty volume path")
	}

	// Volume ID is the same as the volume name in CreateVolume. Serialize by that.
	volumeNameMutex.LockKey(volumeID)
	defer volumeNameMutex.UnlockKey(volumeID)

	// TODO: actually check that the path exists and extract information about it.
	return &csi.NodeGetVolumeStatsResponse{}, nil
}

func (od *oimDriver) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
