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

// RegistryDB stores the mapping from hardware ID to gRPC address of
// the controller for that hardware.
type RegistryDB interface {
	// Store a new mapping. Empty address removes the entry.
	Store(hardwareID, address string)

	// Lookup returns the endpoint or the empty string if not found.
	Lookup(hardwareID string) (address string)
}

// Registry implements oim.Registry.
type Registry struct {
	db RegistryDB
}

func (r *Registry) RegisterController(ctx context.Context, in *oim.RegisterControllerRequest) (*oim.RegisterControllerReply, error) {
	uuid := in.GetUUID()
	if uuid == "" {
		return nil, errors.New("Empty UUID")
	}
	address := in.GetAddress()
	r.db.Store(uuid, address)
	return &oim.RegisterControllerReply{}, nil
}

type Option func(r *Registry) error

func DB(db RegistryDB) Option {
	return func(r *Registry) error {
		r.db = db
		return nil
	}
}

func New(options ...Option) (*Registry, error) {
	r := Registry{}
	r.db = make(MemRegistryDB)
	for _, op := range options {
		err := op(&r)
		if err != nil {
			return nil, err
		}
	}
	return &r, nil
}
