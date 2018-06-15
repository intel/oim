/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcontroller

import (
	"context"
	"errors"
	"fmt"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/spdk"
	"github.com/intel/oim/pkg/spec/oim/v0"
)

// Controller implements oim.Controller.
type Controller struct {
	hardwareID string
	SPDK       *spdk.Client
	vhostSCSI  string
}

func (c *Controller) MapVolume(ctx context.Context, in *oim.MapVolumeRequest) (*oim.MapVolumeReply, error) {
	uuid := in.GetUUID()
	if uuid == "" {
		return nil, errors.New("empty UUID")
	}

	if c.SPDK == nil {
		return nil, errors.New("not connected to SPDK")
	}
	if c.vhostSCSI == "" {
		return nil, errors.New("no VHost SCSI controller configured")
	}

	// Reuse or create BDev.
	if _, err := spdk.GetBDevs(ctx, c.SPDK, spdk.GetBDevsArgs{Name: uuid}); err != nil {
		// TODO: check error more carefully instead of assuming that it merely
		// wasn't found.
		switch x := in.Params.(type) {
		case *oim.MapVolumeRequest_Malloc:
			size := x.Malloc.Size
			args := spdk.ConstructMallocBDevArgs{
				ConstructBDevArgs: spdk.ConstructBDevArgs{
					NumBlocks: size / 512,
					BlockSize: 512,
					Name:      uuid,
				},
			}
			if _, err := spdk.ConstructMallocBDev(ctx, c.SPDK, args); err != nil {
				return nil, errors.New(fmt.Sprintf("ConstructMallocBDev failed: %s", err))
			}
		case *oim.MapVolumeRequest_Ceph:
			return nil, errors.New("not implemented")
		case nil:
			return nil, errors.New("missing volume parameters")
		default:
			return nil, errors.New(fmt.Sprintf("unsupported params type %T", x))
		}
	} else {
		// BDev with the intended name already exists. Assume that it is the right one.
		oimcommon.Infof(1, ctx, "Reusing existing BDev %s", uuid)
	}

	var err error

	// If this BDev is active as LUN, do nothing because a previous MapVolume
	// call must have succeeded (idempotency!).
	controllers, err := spdk.GetVHostControllers(ctx, c.SPDK)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("GetVHostControllers: %s", err))
	}
	for _, controller := range controllers {
		for key, value := range controller.BackendSpecific {
			switch key {
			case "scsi":
				if scsi, ok := value.(spdk.SCSIControllerSpecific); ok {
					for _, target := range scsi {
						for _, lun := range target.LUNs {
							if lun.BDevName == uuid {
								// BDev already active.
								return &oim.MapVolumeReply{}, nil
							}
						}
					}
				}
			}
		}
	}

	// Create a new SCSI target with a LUN connected to this BDev. We iterate over all available
	// targets and attempt to use them.
	// TODO: we don't know the SPDK limit for targets. 8 is just the default.
	// TODO: let vhost pick an unused one (https://github.com/spdk/spdk/issues/328)
	for target := uint32(0); target < 8; target++ {
		args := spdk.AddVHostSCSILUNArgs{
			Controller:    c.vhostSCSI,
			SCSITargetNum: target,
			BDevName:      uuid,
		}
		err = spdk.AddVHostSCSILUN(ctx, c.SPDK, args)
		if err == nil {
			// Success!
			return &oim.MapVolumeReply{}, nil
		}
	}

	// TODO: document that the BDev is not going to get deleted.
	// To remove it, UnmapVolume must be called.

	// Return the last SPDK error.
	errorResult := errors.New(fmt.Sprintf("AddVHostSCSILUN failed for all LUNs, last error: %s", err))
	return nil, errorResult
}

func (c *Controller) UnmapVolume(ctx context.Context, in *oim.UnmapVolumeRequest) (*oim.UnmapVolumeReply, error) {
	uuid := in.GetUUID()
	if uuid == "" {
		return nil, errors.New("empty UUID")
	}

	controllers, err := spdk.GetVHostControllers(ctx, c.SPDK)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("GetVHostControllers: %s", err))
	}
	// For the sake of completeness we keep iterating even after having found
	// something.
	for _, controller := range controllers {
		for key, value := range controller.BackendSpecific {
			switch key {
			case "scsi":
				if scsi, ok := value.(spdk.SCSIControllerSpecific); ok {
					for _, target := range scsi {
						for _, lun := range target.LUNs {
							if lun.BDevName == uuid {
								// Found the right SCSI target.
								removeArgs := spdk.RemoveVHostSCSITargetArgs{
									Controller:    controller.Controller,
									SCSITargetNum: target.SCSIDevNum,
								}
								if err := spdk.RemoveVHostSCSITarget(ctx, c.SPDK, removeArgs); err != nil {
									return nil, errors.New(fmt.Sprintf("RemoveVHostSCSITarget: %s", err))
								}
							}
						}
					}
				}
			}
		}
	}

	// Don't fail when the BDev is not found (idempotency).
	if err := spdk.DeleteBDev(ctx, c.SPDK, spdk.DeleteBDevArgs{Name: uuid}); err != nil {
		// TODO: detect error (https://github.com/spdk/spdk/issues/319)
	}

	return &oim.UnmapVolumeReply{}, nil
}

type Option func(c *Controller) error

func WithHardwareID(hardwareID string) Option {
	return func(c *Controller) error {
		c.hardwareID = hardwareID
		return nil
	}
}

func WithSPDK(path string) Option {
	return func(c *Controller) error {
		if path == "" {
			c.SPDK = nil
			return nil
		}
		client, err := spdk.New(path)
		if err != nil {
			return err
		}
		c.SPDK = client
		return nil
	}
}

func WithVHostController(vhost string) Option {
	return func(c *Controller) error {
		c.vhostSCSI = vhost
		return nil
	}
}

func New(options ...Option) (*Controller, error) {
	c := Controller{
		hardwareID: "unset-hardware-id",
	}
	for _, op := range options {
		err := op(&c)
		if err != nil {
			return nil, err
		}
	}
	return &c, nil
}
