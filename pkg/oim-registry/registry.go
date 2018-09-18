/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimregistry

import (
	"context"
	"errors"
	"strings"

	"github.com/vgough/grpc-proxy/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/spec/oim/v0"
)

// RegistryDB stores the mapping from controller ID to gRPC address of
// the controller.
type RegistryDB interface {
	// Store a new mapping. Empty address removes the entry.
	Store(controllerID, address string)

	// Lookup returns the endpoint or the empty string if not found.
	Lookup(controllerID string) (address string)

	// Foreach iterates over all DB entries until
	// the callback function returns false.
	Foreach(func(controllerID, address string) bool)
}

// Registry implements oim.Registry.
type Registry struct {
	db RegistryDB
}

func (r *Registry) RegisterController(ctx context.Context, in *oim.RegisterControllerRequest) (*oim.RegisterControllerReply, error) {
	controllerID := in.GetControllerId()
	if controllerID == "" {
		return nil, errors.New("Empty controller ID")
	}
	address := in.GetAddress()
	r.db.Store(controllerID, address)
	return &oim.RegisterControllerReply{}, nil
}

func (r *Registry) GetControllers(ctx context.Context, in *oim.GetControllerRequest) (*oim.GetControllerReply, error) {
	out := oim.GetControllerReply{}
	r.db.Foreach(func(controllerID, address string) bool {
		out.Entries = append(out.Entries,
			&oim.DBEntry{
				ControllerId: controllerID,
				Address:      address,
			})
		// More data please...
		return true
	})
	return &out, nil
}

// StreamDirectory transparently proxies gRPC method calls to the
// corresponding controller, without keeping connections open.
func (r *Registry) StreamDirector() proxy.StreamDirector {
	return &StreamDirector{r}
}

type StreamDirector struct {
	r *Registry
}

func (sd *StreamDirector) Connect(ctx context.Context, method string) (context.Context, *grpc.ClientConn, error) {
	// Make sure we never forward internal services.
	if strings.HasPrefix(method, "/oim.v0.Registry/") {
		return nil, nil, status.Error(codes.Unimplemented, "Unknown method")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	// Copy the inbound metadata explicitly.
	outCtx := metadata.NewOutgoingContext(ctx, md.Copy())
	if ok {
		// Decide on which backend to dial
		if controllerID, exists := md["controllerid"]; exists {
			address := sd.r.db.Lookup(controllerID[0])
			if address == "" {
				return outCtx, nil, status.Errorf(codes.Unavailable, "%s: not registered", controllerID[0])
			}
			opts := oimcommon.ChooseDialOpts(address, grpc.WithCodec(proxy.Codec()))

			// Make sure we use DialContext so the dialing can be cancelled/time out together with the context.
			conn, err := grpc.DialContext(ctx, address, opts...)
			return outCtx, conn, err
		}
	}
	return outCtx, nil, status.Error(codes.Unimplemented, "Unknown method")
}

func (sd *StreamDirector) Release(ctx context.Context, conn *grpc.ClientConn) {
	conn.Close()
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

// Creates a server as required to run the registry service.
func Server(endpoint string, registry *Registry) (*oimcommon.NonBlockingGRPCServer, func(*grpc.Server)) {
	service := func(s *grpc.Server) {
		oim.RegisterRegistryServer(s, registry)
	}
	server := &oimcommon.NonBlockingGRPCServer{
		Endpoint: endpoint,
		ServerOptions: []grpc.ServerOption{
			grpc.CustomCodec(proxy.Codec()),
			grpc.UnknownServiceHandler(proxy.TransparentHandler(&StreamDirector{registry})),
		},
	}
	return server, service
}
