/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"google.golang.org/grpc"

	"github.com/intel/oim/pkg/log"
)

// ParseEndpoint splits a string of the format (unix|tcp)://<address> and returns
// the network and address separately.
func ParseEndpoint(ep string) (string, string, error) {
	lower := strings.ToLower(ep)
	if strings.HasPrefix(lower, "unix://") ||
		strings.HasPrefix(lower, "tcp://") ||
		strings.HasPrefix(lower, "tcp4://") ||
		strings.HasPrefix(lower, "tcp6://") {
		s := strings.SplitN(ep, "://", 2)
		if s[1] != "" {
			return s[0], s[1], nil
		}
	}
	return "", "", fmt.Errorf("Invalid endpoint: %v", ep)
}

// NonBlockingGRPCServer provides the common functionatilty for a gRPC server.
type NonBlockingGRPCServer struct {
	Endpoint      string
	ServerOptions []grpc.ServerOption
	wg            sync.WaitGroup
	server        *grpc.Server

	addr net.Addr
}

type RegisterService func(*grpc.Server)

// Start listens on the configured endpoint and runs a gRPC server with
// the given services in the background.
func (s *NonBlockingGRPCServer) Start(ctx context.Context, services ...RegisterService) error {
	proto, addr, err := ParseEndpoint(s.Endpoint)
	if err != nil {
		return errors.Wrap(err, "parse endpoint")
	}

	if proto == "unix" {
		addr = "/" + addr
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			return errors.Wrap(err, "remove Unix socket")
		}
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		return err
	}
	s.addr = listener.Addr()

	logger := log.FromContext(ctx)
	formatter := CompletePayloadFormatter{}

	interceptor := grpc_middleware.ChainUnaryServer(
		otgrpc.OpenTracingServerInterceptor(
			opentracing.GlobalTracer(),
			otgrpc.SpanDecorator(TraceGRPCPayload(formatter))),
		LogGRPCServer(logger, formatter))
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(interceptor),
	}
	opts = append(opts, s.ServerOptions...)
	server := grpc.NewServer(opts...)
	s.server = server

	for _, service := range services {
		service(server)
	}

	logger.Infow("listening for connections", "address", listener.Addr())

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		server.Serve(listener)
	}()
	return nil
}

// Addr returns the address on which the server is listening, nil if none.
// Can be used to find the actual port when using tcp://:0 as endpoint.
func (s *NonBlockingGRPCServer) Addr() net.Addr {
	return s.addr
}

// Wait for completion of the background server.
func (s *NonBlockingGRPCServer) Wait(ctx context.Context) {
	s.wg.Wait()
	s.addr = nil
}

// Stop the background server, allowing it to finish current requests.
func (s *NonBlockingGRPCServer) Stop(ctx context.Context) {
	s.server.GracefulStop()
}

// ForceStop stops the background server immediately.
func (s *NonBlockingGRPCServer) ForceStop(ctx context.Context) {
	s.server.Stop()
}

// Run combines Start and Wait.
func (s *NonBlockingGRPCServer) Run(ctx context.Context, services ...RegisterService) error {
	if err := s.Start(ctx, services...); err != nil {
		return err
	}
	s.Wait(ctx)
	return nil
}
