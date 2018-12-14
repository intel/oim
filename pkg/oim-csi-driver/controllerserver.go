/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
)

const (
	maxStorageCapacity = tib
)

func (od *oimDriver) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	name := req.GetName()
	caps := req.GetVolumeCapabilities()

	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "Name missing in request")
	}
	if caps == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume Capabilities missing in request")
	}
	for _, cap := range caps {
		if cap.GetBlock() != nil {
			return nil, status.Error(codes.Unimplemented, "Block Volume not supported")
		}
		switch cap.GetAccessMode().GetMode() {
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER: // okay
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY: // okay
		case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY: // okay

		case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
			// While in theory writing blocks on one node and reading them on others could work,
			// in practice caching effects might break that. Better don't allow it.
			return nil, status.Error(codes.Unimplemented, "multi-node reader, single writer not supported")
		case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			return nil, status.Error(codes.Unimplemented, "multi-node reader, multi-node writer not supported")
		default:
			return nil, status.Error(codes.Unimplemented, fmt.Sprintf("%s not supported", cap.GetAccessMode().GetMode()))
		}
	}
	if req.GetVolumeContentSource() != nil {
		return nil, status.Error(codes.Unimplemented, "snapshots not supported")
	}

	// Serialize operations per volume by name.
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "empty name")
	}
	volumeNameMutex.LockKey(name)
	defer volumeNameMutex.UnlockKey(name)

	if od.vhostEndpoint != "" {
		return od.createVolumeSPDK(ctx, req)
	}
	return od.createVolumeOIM(ctx, req)
}

func (od *oimDriver) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	// Volume ID is the same as the volume name in CreateVolume. Serialize by that.
	name := req.GetVolumeId()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "empty volume ID")
	}
	volumeNameMutex.LockKey(name)
	defer volumeNameMutex.UnlockKey(name)

	if od.vhostEndpoint != "" {
		return od.deleteVolumeSPDK(ctx, req)
	}
	return od.deleteVolumeOIM(ctx, req)
}

func (od *oimDriver) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (od *oimDriver) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (od *oimDriver) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if req.GetVolumeCapabilities() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities missing in request")
	}

	// Volume ID is the same as the volume name in CreateVolume. Serialize by that.
	name := req.GetVolumeId()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "empty volume ID")
	}
	volumeNameMutex.LockKey(name)
	defer volumeNameMutex.UnlockKey(name)

	// Check that volume exists.
	if err := od.checkVolumeExists(ctx, req.GetVolumeId()); err != nil {
		return nil, err
	}

	for _, cap := range req.VolumeCapabilities {
		if cap.GetAccessMode().GetMode() != csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER {
			return &csi.ValidateVolumeCapabilitiesResponse{Supported: false, Message: ""}, nil
		}
	}
	return &csi.ValidateVolumeCapabilitiesResponse{Supported: true, Message: ""}, nil
}

func (od *oimDriver) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (od *oimDriver) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerGetCapabilities implements the default GRPC callout.
// Default supports all capabilities
func (od *oimDriver) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: od.cap,
	}, nil
}

func (od *oimDriver) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (od *oimDriver) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (od *oimDriver) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
