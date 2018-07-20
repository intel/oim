/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Coporation.

SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"context"
	"flag"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-registry"
)

var (
	endpoint = flag.String("endpoint", "unix:///tmp/registry.sock", "OIM registry endpoint")
	_        = log.InitSimpleFlags()
)

func main() {
	flag.Parse()
	app := "oim-registry"

	logger := log.NewSimpleLogger(log.NewSimpleConfig())
	log.Set(logger)

	closer, err := oimcommon.InitTracer(app)
	if err != nil {
		logger.Fatalf("Failed to initialize tracer: %s\n", err)
	}
	defer closer.Close()

	registry, err := oimregistry.New()
	if err != nil {
		logger.Fatalf("Failed to initialize server: %s\n", err)
	}
	server, service := oimregistry.Server(*endpoint, registry)
	if err := server.Run(context.Background(), service); err != nil {
		logger.Fatalf("Failed to run server: %s\n", err)
	}
}
