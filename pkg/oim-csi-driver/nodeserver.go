/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"context"
	"os"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/mount"
)

// Name is specified by generated interface, can't make it NodeGetID.
// nolint: golint
func (od *oimDriver) NodeGetId(ctx context.Context, req *csi.NodeGetIdRequest) (*csi.NodeGetIdResponse, error) {
	return &csi.NodeGetIdResponse{
		NodeId: od.nodeID,
	}, nil
}

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
						Type: csi.NodeServiceCapability_RPC_UNKNOWN,
					},
				},
			},
		},
	}, nil
}

func (od *oimDriver) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	// Check arguments
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability missing in request")
	}
	if len(req.GetTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	// Volume ID is the same as the volume name in CreateVolume. Serialize by that.
	name := req.GetVolumeId()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "empty volume ID")
	}
	volumeNameMutex.LockKey(name)
	defer volumeNameMutex.UnlockKey(name)

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

	log.FromContext(ctx).Infow("mounting",
		"target", targetPath,
		"fstype", fsType,
		"read-only", readOnly,
		"volumeid", volumeID,
		"attributes", attrib,
		"flags", mountFlags,
	)

	device, cleanup, err := od.createDevice(ctx, req)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	options := []string{}
	if readOnly {
		options = append(options, "ro")
	}
	diskMounter := &mount.SafeFormatAndMount{Interface: mount.New(""), Exec: mount.NewOsExec()}
	if err := diskMounter.FormatAndMount(device, targetPath, fsType, options); err != nil {
		// We get a pretty bad error code from FormatAndMount ("exit code 1") :-/
		return nil, errors.Wrapf(err, "formatting as %s and mounting %s at %s", fsType, device, targetPath)
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (od *oimDriver) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}
	targetPath := req.GetTargetPath()
	volumeID := req.GetVolumeId()

	// Volume ID is the same as the volume name in CreateVolume. Serialize by that.
	name := req.GetVolumeId()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "empty volume ID")
	}
	volumeNameMutex.LockKey(name)
	defer volumeNameMutex.UnlockKey(name)

	// Unmounting the image
	// TODO: check whether this really is still a mount point. We might have removed it already.
	log.FromContext(ctx).Infow("unmount",
		"target", targetPath,
		"volumeid", volumeID,
	)
	if err := mount.New("").Unmount(req.GetTargetPath()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if err := od.deleteDevice(ctx, volumeID); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (od *oimDriver) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetStagingTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (od *oimDriver) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetStagingTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}
