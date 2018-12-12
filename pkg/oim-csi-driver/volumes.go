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

	"google.golang.org/grpc/metadata"

	"github.com/intel/oim/pkg/spdk"
	"github.com/intel/oim/pkg/spec/oim/v0"
)

func (od *oimDriver) createVolumeSPDK(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	// Connect to SPDK.
	client, err := spdk.New(od.vhostEndpoint)
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

func (od *oimDriver) createVolumeOIM(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	// Check for maximum available capacity
	capacity := int64(req.GetCapacityRange().GetRequiredBytes())
	if capacity >= maxStorageCapacity {
		return nil, status.Errorf(codes.OutOfRange, "Requested capacity %d exceeds maximum allowed %d", capacity, maxStorageCapacity)
	}

	// If capacity is unset, round up to minimum size (1MB?).
	if capacity == 0 {
		capacity = mib
	}

	if err := od.provisionOIM(ctx, req.GetName(), capacity); err != nil {
		return nil, err
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

func (od *oimDriver) deleteVolumeSPDK(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	// Connect to SPDK.
	client, err := spdk.New(od.vhostEndpoint)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to connect to SPDK: %s", err))
	}
	defer client.Close()

	// We must not error out when the BDev does not exist (might have been deleted already).
	// TODO: proper detection of "bdev not found" (https://github.com/spdk/spdk/issues/319).
	volumeID := req.VolumeId
	if err := spdk.DeleteBDev(ctx, client, spdk.DeleteBDevArgs{Name: volumeID}); err != nil && !spdk.IsJSONError(err, spdk.ERROR_INVALID_PARAMS) {
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to delete SPDK Malloc BDev %s: %s", volumeID, err))
	}
	return &csi.DeleteVolumeResponse{}, nil
}

func (od *oimDriver) deleteVolumeOIM(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if err := od.provisionOIM(ctx, req.GetVolumeId(), 0); err != nil {
		return nil, err
	}
	return &csi.DeleteVolumeResponse{}, nil

}

func (od *oimDriver) provisionOIM(ctx context.Context, bdevName string, size int64) error {
	// Connect to OIM controller through OIM registry.
	conn, err := od.DialRegistry(ctx)
	if err != nil {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	defer conn.Close()
	controllerClient := oim.NewControllerClient(conn)
	ctx = metadata.AppendToOutgoingContext(ctx, "controllerid", od.oimControllerID)
	_, err = controllerClient.ProvisionMallocBDev(ctx, &oim.ProvisionMallocBDevRequest{
		BdevName: bdevName,
		Size_:    size,
	})
	return err
}

func (od *oimDriver) checkVolumeExists(ctx context.Context, volumeID string) error {
	if od.vhostEndpoint != "" {
		return od.checkVolumeExistsSPDK(ctx, volumeID)
	}
	return od.checkVolumeExistsOIM(ctx, volumeID)
}

func (od *oimDriver) checkVolumeExistsSPDK(ctx context.Context, volumeID string) error {
	// Connect to SPDK.
	client, err := spdk.New(od.vhostEndpoint)
	if err != nil {
		return status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to connect to SPDK: %s", err))
	}
	defer client.Close()

	bdevs, err := spdk.GetBDevs(ctx, client, spdk.GetBDevsArgs{Name: volumeID})
	if err == nil && len(bdevs) == 1 {
		return nil
	}

	// TODO: detect "not found" error (https://github.com/spdk/spdk/issues/319)
	return status.Error(codes.NotFound, "")
}

func (od *oimDriver) checkVolumeExistsOIM(ctx context.Context, volumeID string) error {
	// Connect to OIM controller through OIM registry.
	conn, err := od.DialRegistry(ctx)
	if err != nil {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	defer conn.Close()
	controllerClient := oim.NewControllerClient(conn)
	ctx = metadata.AppendToOutgoingContext(ctx, "controllerid", od.oimControllerID)
	_, err = controllerClient.CheckMallocBDev(ctx, &oim.CheckMallocBDevRequest{
		BdevName: volumeID,
	})
	return err
}
