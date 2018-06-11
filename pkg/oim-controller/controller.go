/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcontroller

import (
	"context"
	"errors"

	"github.com/intel/oim/pkg/spec/oim/v0"
)

// controller implements oim.Controller.
type controller struct {
	hardwareID string
}

func (c *controller) MapVolume(ctx context.Context, in *oim.MapVolumeRequest) (*oim.MapVolumeReply, error) {
	return nil, errors.New("not implemented")
}

func (c *controller) UnmapVolume(ctx context.Context, in *oim.UnmapVolumeRequest) (*oim.UnmapVolumeReply, error) {
	return nil, errors.New("not implemented")
}

type Option func(c *controller) error

func OptionHardwareID(hardwareID string) Option {
	return func(c *controller) error {
		c.hardwareID = hardwareID
		return nil
	}
}

func New(options ...Option) (*controller, error) {
	c := controller{
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
