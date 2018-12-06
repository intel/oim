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
	version      = "unknown" // set at build time
	printVersion = flag.Bool("version", false, "output version information and exit")
	endpoint     = flag.String("endpoint", "unix:///tmp/registry.sock", "OIM registry endpoint")
	ca           = flag.String("ca", "", "the required CA's .crt file which is used for verifying connections")
	key          = flag.String("key", "", "the base name of the required .key and .crt files that authenticate and authorize the registry")
	_            = log.InitSimpleFlags()
)

func main() {
	flag.Parse()
	app := "oim-registry"

	logger := log.NewSimpleLogger(log.NewSimpleConfig())
	log.Set(logger)

	if *printVersion {
		logger.Infof("oim-registry %s", version)
		return
	}

	if *ca == "" {
		logger.Fatalf("A CA file is required.")
	}
	if *key == "" {
		logger.Fatalf("A key file is required.")
	}

	closer, err := oimcommon.InitTracer(app)
	if err != nil {
		logger.Fatalf("Failed to initialize tracer: %s\n", err)
	}
	defer closer.Close()

	tlsConfig, err := oimcommon.LoadTLSConfig(*ca, *key, "")
	if err != nil {
		logger.Fatalw("load TLS certs", "error", err)
	}

	registry, err := oimregistry.New(oimregistry.TLS(tlsConfig))
	if err != nil {
		logger.Fatalf("Failed to initialize server: %s\n", err)
	}
	server, service := registry.Server(*endpoint)
	if err := server.Run(context.Background(), service); err != nil {
		logger.Fatalf("Failed to run server: %s\n", err)
	}
}
