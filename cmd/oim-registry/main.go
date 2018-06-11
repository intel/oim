/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Coporation.

SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"flag"
	"fmt"
	"os"

	"google.golang.org/grpc"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-registry"
	"github.com/intel/oim/pkg/spec/oim/v0"
)

func init() {
	flag.Set("logtostderr", "true")
}

var (
	endpoint = flag.String("endpoint", "unix:///tmp/registry.sock", "OIM registry endpoint")
)

func main() {
	flag.Parse()
	closer, err := oimcommon.InitTracer("OIM Registry")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize tracer: %s\n", err)
		os.Exit(1)
	}
	defer closer.Close()

	registry, err := oimregistry.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize server: %s\n", err)
		os.Exit(1)
	}
	server := oimcommon.NonBlockingGRPCServer{
		Endpoint: *endpoint,
	}
	service := func(s *grpc.Server) {
		oim.RegisterRegistryServer(s, registry)
	}
	if err := server.Run(service); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to run server: %s\n", err)
		os.Exit(1)
	}
}
