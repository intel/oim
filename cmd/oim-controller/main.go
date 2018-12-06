/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Coporation.

SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"context"
	"flag"
	"time"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-controller"
)

var (
	version           = "unknown" // set at build time
	printVersion      = flag.Bool("version", false, "output version information and exit")
	endpoint          = flag.String("endpoint", "tcp://:8999", "OIM controller endpoint for net.Listen")
	spdk              = flag.String("spdk", "/var/tmp/vhost.sock", "SPDK VHost RPC socket path")
	vhost             = flag.String("vhost-scsi-controller", "vhost.0", "SPDK VirtIO SCSI controller name")
	vhostDev          = flag.String("vm-vhost-device", "", "the PCI address of the SCSI controller in a VM ([domain:]bus:device.function), partial address allowed (:.3)")
	controllerID      = flag.String("controllerid", "", "unique id for this controller instance")
	controllerAddress = flag.String("controller-address", "ipv4:///oim-controller:8999", "external gRPC name for use with grpc.Dial that corresponds to the endpoint")
	registry          = flag.String("registry", "", "gRPC name that connects to the OIM registry, empty disables registration")
	ca                = flag.String("ca", "", "the required CA's .crt file which is used for verifying connections to the registry")
	key               = flag.String("key", "", "the base name of the required .key and .crt files that authenticate and authorize the registry client")
	registryDelay     = flag.Duration("registry-delay", time.Minute, "determines how long the controller waits before registering at the OIM registry")
	_                 = log.InitSimpleFlags()
)

func main() {
	flag.Parse()
	app := "oim-controller"

	logger := log.NewSimpleLogger(log.NewSimpleConfig())
	log.Set(logger)

	if *printVersion {
		logger.Infof("oim-controller %s", version)
		return
	}

	closer, err := oimcommon.InitTracer(app)
	if err != nil {
		logger.Fatalf("Failed to initialize tracer: %s\n", err)
	}
	defer closer.Close()

	transportCreds, err := oimcommon.LoadTLS(*ca, *key, "component.registry")
	if err != nil {
		logger.Fatalw("load TLS certs", "error", err)
	}

	options := []oimcontroller.Option{
		oimcontroller.WithControllerID(*controllerID),
		oimcontroller.WithSPDK(*spdk),
		oimcontroller.WithVHostController(*vhost),
		oimcontroller.WithVHostDev(*vhostDev),
		oimcontroller.WithControllerAddress(*controllerAddress),
		oimcontroller.WithRegistry(*registry),
		oimcontroller.WithRegistryDelay(*registryDelay),
		oimcontroller.WithCreds(transportCreds),
	}
	controller, err := oimcontroller.New(options...)
	if err != nil {
		logger.Fatalf("Failed to initialize server: %s\n", err)
	}
	if err := controller.Start(); err != nil {
		logger.Fatalf("Failed to start auto-registrationg: %s\n", err)
	}
	defer controller.Stop()
	server, service := controller.Server(*endpoint)
	if err := server.Run(context.Background(), service); err != nil {
		logger.Fatalf("Failed to run server: %s\n", err)
	}
}
