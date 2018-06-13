/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/golang/glog"
	"google.golang.org/grpc"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/opentracing/opentracing-go"
)

// ParseEndpoint splits a string of the format (unix|tcp)://<address> and returns
// the network and address separately.
func ParseEndpoint(ep string) (string, string, error) {
	if strings.HasPrefix(strings.ToLower(ep), "unix://") || strings.HasPrefix(strings.ToLower(ep), "tcp://") {
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
}

type RegisterService func(*grpc.Server)

// Start listens on the configured endpoint and runs a gRPC server with
// the given services in the background.
func (s *NonBlockingGRPCServer) Start(services ...RegisterService) error {

	proto, addr, err := ParseEndpoint(s.Endpoint)
	if err != nil {
		glog.Fatal(err.Error())
	}

	if proto == "unix" {
		addr = "/" + addr
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		return err
	}

	interceptor := grpc_middleware.ChainUnaryServer(
		otgrpc.OpenTracingServerInterceptor(
			opentracing.GlobalTracer(),
			otgrpc.SpanDecorator(TraceGRPCPayload)),
		LogGRPCServer)
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(interceptor),
	}
	opts = append(opts, s.ServerOptions...)
	server := grpc.NewServer(opts...)
	s.server = server

	for _, service := range services {
		service(server)
	}

	glog.Infof("Listening for connections on address: %#v", listener.Addr())

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		server.Serve(listener)
	}()
	return nil
}

// Wait for completion of the background server.
func (s *NonBlockingGRPCServer) Wait() {
	s.wg.Wait()
}

// Stop the background server, allowing it to finish current requests.
func (s *NonBlockingGRPCServer) Stop() {
	s.server.GracefulStop()
}

// ForceStop stops the background server immediately.
func (s *NonBlockingGRPCServer) ForceStop() {
	s.server.Stop()
}

// Run combines Start and Wait.
func (s *NonBlockingGRPCServer) Run(services ...RegisterService) error {
	if err := s.Start(services...); err != nil {
		return err
	}
	s.Wait()
	return nil
}
