/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"fmt"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/spdk"
)

const (
	maxStorageCapacity = tib
)

type controllerServer struct {
	*DefaultControllerServer
	od *oimDriver
}

func (cs *controllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if err := cs.Driver.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		oimcommon.Infof(3, ctx, "invalid create volume req: %v", req)
		return nil, err
	}

	// Check arguments
	if len(req.GetName()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Name missing in request")
	}
	if req.GetVolumeCapabilities() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume Capabilities missing in request")
	}

	// Connect to SPDK.
	// TODO: make this configurable and decide whether we need to
	// talk to a local or remote SPDK.
	client, err := spdk.New(cs.od.vhostEndpoint)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to connect to SPDK: %s", err))
	}
	defer client.Close()

	// Need to check for already existing volume name, and if found
	// check for the requested capacity and already allocated capacity
	bdevs, err := spdk.GetBDevs(ctx, client, spdk.GetBDevsArgs{Name: req.GetName()})
	if err == nil && len(bdevs) == 1 {
		bdev := bdevs[0]
		// Since err is nil, it means the volume with the same name already exists
		// need to check if the size of exisiting volume is the same as in new
		// request
		volSize := bdev.BlockSize * bdev.NumBlocks
		if volSize >= int64(req.GetCapacityRange().GetRequiredBytes()) {
			// exisiting volume is compatible with new request and should be reused.
			return &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					Id:            req.GetName(),
					CapacityBytes: int64(volSize),
					Attributes:    req.GetParameters(),
				},
			}, nil
		}
		return nil, status.Error(codes.AlreadyExists, fmt.Sprintf("Volume with the same name: %s but with different size already exist", req.GetName()))
	}
	// If we get an error, we might have a problem or the bdev simply doesn't exist.
	// A bit hard to tell, unfortunately (see https://github.com/spdk/spdk/issues/319).
	if err != nil && !spdk.IsJSONError(err, spdk.ERROR_INVALID_PARAMS) {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to get BDevs from SPDK: %s", err))
	}

	// Check for maximum available capacity
	capacity := int64(req.GetCapacityRange().GetRequiredBytes())
	if capacity >= maxStorageCapacity {
		return nil, status.Errorf(codes.OutOfRange, "Requested capacity %d exceeds maximum allowed %d", capacity, maxStorageCapacity)
	}

	// If capacity is unset, round up to minimum size (1MB?).
	if capacity == 0 {
		capacity = mib
	}

	// Create new Malloc bdev.
	args := spdk.ConstructMallocBDevArgs{ConstructBDevArgs: spdk.ConstructBDevArgs{
		NumBlocks: capacity / 512,
		BlockSize: 512,
		Name:      req.GetName(),
	}}
	_, err = spdk.ConstructMallocBDev(ctx, client, args)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to create SPDK Malloc BDev: %s", err))
	}
	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			// We use the unique name also as ID.
			Id:            req.GetName(),
			CapacityBytes: req.GetCapacityRange().GetRequiredBytes(),
			Attributes:    req.GetParameters(),
		},
	}, nil
}

func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	// Connect to SPDK.
	// TODO: make this configurable and decide whether we need to
	// talk to a local or remote SPDK.
	client, err := spdk.New(cs.od.vhostEndpoint)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to connect to SPDK: %s", err))
	}
	defer client.Close()

	if err := cs.Driver.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		oimcommon.Infof(3, ctx, "invalid delete volume req: %v", req)
		return nil, err
	}
	// We must not error out when the BDev does not exist (might have been deleted already).
	// TODO: proper detection of "bdev not found" (https://github.com/spdk/spdk/issues/319).
	volumeID := req.VolumeId
	if err := spdk.DeleteBDev(ctx, client, spdk.DeleteBDevArgs{Name: volumeID}); err != nil && !spdk.IsJSONError(err, spdk.ERROR_INVALID_PARAMS) {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to delete SPDK Malloc BDev %s: %s", volumeID, err))
	}
	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if req.GetVolumeCapabilities() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities missing in request")
	}

	for _, cap := range req.VolumeCapabilities {
		if cap.GetAccessMode().GetMode() != csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER {
			return &csi.ValidateVolumeCapabilitiesResponse{Supported: false, Message: ""}, nil
		}
	}
	return &csi.ValidateVolumeCapabilitiesResponse{Supported: true, Message: ""}, nil
}
