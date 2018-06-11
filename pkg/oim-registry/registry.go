/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimregistry

import (
	"context"
	"errors"

	"github.com/intel/oim/pkg/spec/oim/v0"
)

// registry implements oim.Registry.
type registry struct {
}

func (r *registry) RegisterController(ctx context.Context, in *oim.RegisterControllerRequest) (*oim.RegisterControllerReply, error) {
	return nil, errors.New("not implemented")
}

type Option func(r *registry) error

func New(options ...Option) (*registry, error) {
	c := registry{}
	for _, op := range options {
		err := op(&c)
		if err != nil {
			return nil, err
		}
	}
	return &c, nil
}
