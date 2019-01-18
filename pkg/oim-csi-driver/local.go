/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"context"
	"fmt"
	"os"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/pkg/errors"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/spdk"
)

type localSPDK struct {
	vhostEndpoint string
}

var _ OIMBackend = &localSPDK{}

func (l *localSPDK) createVolume(ctx context.Context, volumeID string, requiredBytes int64) (int64, error) {
	// Connect to SPDK.
	client, err := spdk.New(l.vhostEndpoint)
	if err != nil {
		return 0, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to connect to SPDK: %s", err))
	}
	defer client.Close()

	// Need to check for already existing volume name, and if found
	// check for the requested capacity and already allocated capacity
	bdevs, err := spdk.GetBDevs(ctx, client, spdk.GetBDevsArgs{Name: volumeID})
	if err == nil && len(bdevs) == 1 {
		bdev := bdevs[0]
		// Since err is nil, it means the volume with the same name already exists
		// need to check if the size of exisiting volume is the same as in new
		// request
		volSize := bdev.BlockSize * bdev.NumBlocks
		if volSize >= requiredBytes {
			// exisiting volume is compatible with new request and should be reused.
			return volSize, nil
		}
		return 0, status.Error(codes.AlreadyExists, fmt.Sprintf("Volume with the same name: %s but with different size already exist", volumeID))
	}
	// If we get an error, we might have a problem or the bdev simply doesn't exist.
	// A bit hard to tell, unfortunately (see https://github.com/spdk/spdk/issues/319).
	if err != nil && !spdk.IsJSONError(err, spdk.ERROR_INVALID_PARAMS) {
		return 0, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to get BDevs from SPDK: %s", err))
	}

	// Check for maximum available capacity
	capacity := requiredBytes
	if capacity >= maxStorageCapacity {
		return 0, status.Errorf(codes.OutOfRange, "Requested capacity %d exceeds maximum allowed %d", capacity, maxStorageCapacity)
	}

	if capacity == 0 {
		// If capacity is unset, round up to minimum size (1MB?).
		capacity = mib
	} else {
		// Round up to multiple of 512.
		capacity = (capacity + 511) / 512 * 512
	}

	// Create new Malloc bdev.
	args := spdk.ConstructMallocBDevArgs{ConstructBDevArgs: spdk.ConstructBDevArgs{
		NumBlocks: capacity / 512,
		BlockSize: 512,
		Name:      volumeID,
	}}
	_, err = spdk.ConstructMallocBDev(ctx, client, args)
	if err != nil {
		return 0, status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to create SPDK Malloc BDev: %s", err))
	}
	return capacity, nil
}

func (l *localSPDK) deleteVolume(ctx context.Context, volumeID string) error {
	// Connect to SPDK.
	client, err := spdk.New(l.vhostEndpoint)
	if err != nil {
		return status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to connect to SPDK: %s", err))
	}
	defer client.Close()

	// We must not error out when the BDev does not exist (might have been deleted already).
	// TODO: proper detection of "bdev not found" (https://github.com/spdk/spdk/issues/319).
	if err := spdk.DeleteBDev(ctx, client, spdk.DeleteBDevArgs{Name: volumeID}); err != nil && !spdk.IsJSONError(err, spdk.ERROR_INVALID_PARAMS) {
		return status.Error(codes.FailedPrecondition, fmt.Sprintf("Failed to delete SPDK Malloc BDev %s: %s", volumeID, err))
	}
	return nil
}

func (l *localSPDK) checkVolumeExists(ctx context.Context, volumeID string) error {
	// Connect to SPDK.
	client, err := spdk.New(l.vhostEndpoint)
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

func (l *localSPDK) createDevice(ctx context.Context, volumeID string, request interface{}) (string, cleanup, error) {
	// Connect to SPDK.
	client, err := spdk.New(l.vhostEndpoint)
	if err != nil {
		return "", nil, errors.Wrap(err, "connect to SPDK")
	}
	defer client.Close()

	// We might have already mapped that BDev to a NBD disk - check!
	nbdDevice, err := findNBDDevice(ctx, client, volumeID)
	if err != nil {
		return "", nil, errors.Wrap(err, "find NBD device")
	}
	if nbdDevice != "" {
		log.FromContext(ctx).Infof("Reusing already started NBD disk: %s", nbdDevice)
	} else {
		var nbdError error
		// Find a free NBD device node and start a NBD disk there.
		// Unfortunately this is racy. We assume that we are the
		// only users of /dev/nbd*.
		for i := 0; ; i++ {
			// Filename from variable is save here.
			n := fmt.Sprintf("/dev/nbd%d", i)
			nbdFile, err := os.Open(n) // nolint: gosec

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
			if err != nil {
				nbdError = err
				continue
			}
			err = nbdFile.Close()
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
			return "", nil, errors.Wrap(nbdError, "no unused /dev/nbd*")
		}
	}

	args := spdk.StartNBDDiskArgs{
		BDevName:  volumeID,
		NBDDevice: nbdDevice,
	}
	if err := spdk.StartNBDDisk(ctx, client, args); err != nil {
		return "", nil, errors.Wrapf(err, "start SPDK NBD disk %+v", args)
	}
	return nbdDevice, nil, nil
}

func (l *localSPDK) deleteDevice(ctx context.Context, volumeID string) error {
	// Connect to SPDK.
	client, err := spdk.New(l.vhostEndpoint)
	if err != nil {
		return errors.Wrap(err, "connect to SPDK")
	}
	defer client.Close()

	// Stop NBD disk.
	nbdDevice, err := findNBDDevice(ctx, client, volumeID)
	if err != nil {
		return errors.Wrap(err, "get NDB disks from SPDK")
	}
	args := spdk.StopNBDDiskArgs{NBDDevice: nbdDevice}
	if err := spdk.StopNBDDisk(ctx, client, args); err != nil {
		return errors.Wrapf(err, "stop SPDK NDB disk %+v", args)
	}
	return nil
}

func findNBDDevice(ctx context.Context, client *spdk.Client, volumeID string) (nbdDevice string, err error) {
	nbdDisks, err := spdk.GetNBDDisks(ctx, client)
	if err != nil {
		return "", errors.Wrap(err, "get NDB disks from SPDK")
	}
	for _, nbd := range nbdDisks {
		if nbd.BDevName == volumeID {
			return nbd.NBDDevice, nil
		}
	}
	return "", nil
}
