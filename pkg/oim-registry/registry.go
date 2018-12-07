/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimregistry

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"github.com/vgough/grpc-proxy/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
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

// GetRegistryEntries returns all database entries as a map.
func GetRegistryEntries(db RegistryDB) map[string]string {
	entries := make(map[string]string)
	db.Foreach(func(key, value string) bool {
		entries[key] = value
		return true
	})
	return entries
}

// Registry implements oim.Registry.
type Registry struct {
	db        RegistryDB
	tlsConfig *tls.Config
}

func getPeer(ctx context.Context) (string, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return "", status.Error(codes.FailedPrecondition, "cannot determine caller identity")
	}
	tlsInfo, ok := peer.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return "", status.Error(codes.FailedPrecondition, "no TLS info")
	}
	if len(tlsInfo.State.VerifiedChains) == 0 ||
		len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return "", status.Error(codes.FailedPrecondition, "cannot determine peer, empty TLS verification chain")
	}
	commonName := tlsInfo.State.VerifiedChains[0][0].Subject.CommonName
	return commonName, nil
}

func (r *Registry) SetValue(ctx context.Context, in *oim.SetValueRequest) (*oim.SetValueReply, error) {
	value := in.GetValue()
	if value == nil {
		return nil, errors.New("missing value")
	}

	// sanitize path
	elements, err := oimcommon.SplitRegistryPath(value.Path)
	if err != nil {
		return nil, err
	}
	if len(elements) == 0 {
		return nil, errors.New("empty path")
	}
	key := oimcommon.JoinRegistryPath(elements)

	// Permission check: admin can set anything, controller only '<controller ID>/address'.
	peer, err := getPeer(ctx)
	if err != nil {
		return nil, err
	}
	allowed := peer == "user.admin" ||
		peer == "controller."+elements[0] && len(elements) == 2 && elements[1] == oimcommon.RegistryAddress
	if !allowed {
		return nil, status.Errorf(codes.PermissionDenied, "caller %q not allowed to set %q", peer, key)
	}

	r.db.Store(key, value.Value)
	return &oim.SetValueReply{}, nil
}

func (r *Registry) GetValues(ctx context.Context, in *oim.GetValuesRequest) (*oim.GetValuesReply, error) {
	// sanitize path
	elements, err := oimcommon.SplitRegistryPath(in.GetPath())
	if err != nil {
		return nil, err
	}
	prefix := oimcommon.JoinRegistryPath(elements)

	// Permission check: everyone can read, but we want to at least know that
	// we have identified a peer (i.e. TLS is active).
	if _, err := getPeer(ctx); err != nil {
		return nil, err
	}

	out := oim.GetValuesReply{}
	r.db.Foreach(func(key, value string) bool {
		if prefix == "" ||
			strings.HasPrefix(key, prefix) &&
				(len(key) == len(prefix) ||
					key[len(prefix)] == '/') {
			out.Values = append(out.Values,
				&oim.Value{
					Path:  key,
					Value: value,
				})
		}
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
		return nil, nil, status.Error(codes.Unimplemented, "unknown method")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, nil, status.Error(codes.FailedPrecondition, "missing metadata")
	}

	// Decide on which backend to dial
	controllerIDs, exists := md["controllerid"]
	if !exists || len(controllerIDs) != 1 {
		return nil, nil, status.Error(codes.FailedPrecondition, "missing or invalid controllerid meta data")
	}
	controllerID := controllerIDs[0]

	// Permission check: only the host service with the same
	// controller ID can contact the controller.
	peer, err := getPeer(ctx)
	if err != nil {
		return nil, nil, err
	}
	prefix := "host."
	if !strings.HasPrefix(peer, prefix) ||
		peer[len(prefix):] != controllerID {
		return nil, nil, status.Errorf(codes.PermissionDenied, "caller %q not allowed to contact controller %q", peer, controllerID)
	}

	address := sd.r.db.Lookup(controllerID + "/" + oimcommon.RegistryAddress)
	if address == "" {
		return nil, nil, status.Errorf(codes.Unavailable, "%s: no address registered", controllerID)
	}

	// We check the controller's common name to ensure that we talk to the right service
	// and not some man-in-the-middle attacker, or simply use the wrong address.
	outgoingTLS := sd.r.tlsConfig.Clone()
	outgoingTLS.ServerName = fmt.Sprintf("controller.%s", controllerID)
	creds := credentials.NewTLS(outgoingTLS)
	opts := oimcommon.ChooseDialOpts(address, grpc.WithCodec(proxy.Codec()), grpc.WithTransportCredentials(creds))

	// Copy the inbound metadata explicitly.
	outCtx := metadata.NewOutgoingContext(ctx, md.Copy())

	// Make sure we use DialContext so the dialing can be cancelled/time out together with the context.
	conn, err := grpc.DialContext(ctx, address, opts...)
	return outCtx, conn, err
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

func TLS(tlsConfig *tls.Config) Option {
	return func(r *Registry) error {
		r.tlsConfig = tlsConfig
		return nil
	}
}

func New(options ...Option) (*Registry, error) {
	r := Registry{}
	r.db = NewMemRegistryDB()
	for _, op := range options {
		err := op(&r)
		if err != nil {
			return nil, err
		}
	}
	if r.tlsConfig == nil {
		return nil, errors.New("transport credentials missing")
	}
	return &r, nil
}

// Creates a server as required to run the registry service.
func (r *Registry) Server(endpoint string) (*oimcommon.NonBlockingGRPCServer, func(*grpc.Server)) {
	service := func(s *grpc.Server) {
		oim.RegisterRegistryServer(s, r)
	}
	server := &oimcommon.NonBlockingGRPCServer{
		Endpoint: endpoint,
		ServerOptions: []grpc.ServerOption{
			grpc.CustomCodec(proxy.Codec()),
			grpc.UnknownServiceHandler(proxy.TransparentHandler(&StreamDirector{r})),
			grpc.Creds(credentials.NewTLS(r.tlsConfig)),
		},
	}
	return server, service
}
