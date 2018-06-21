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
	"time"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-controller"
)

func init() {
	flag.Set("logtostderr", "true")
}

var (
	endpoint          = flag.String("endpoint", "tcp://:8999", "OIM controller endpoint for net.Listen")
	spdk              = flag.String("spdk", "/var/tmp/vhost.sock", "SPDK VHost RPC socket path")
	vhost             = flag.String("vhost-scsi-controller", "vhost.0", "SPDK VirtIO SCSI controller name")
	vhostDev          = flag.String("vm-vhost-device", "", "a unique substring identifying the vhost controller in the virtual machine's /sys/dev/block directory")
	controllerID      = flag.String("controllerid", "", "unique id for this controller instance")
	controllerAddress = flag.String("controller-address", "ipv4:///oim-controller:8999", "external gRPC name for use with grpc.Dial that corresponds to the endpoint")
	registry          = flag.String("registry", "", "gRPC name that connects to the OIM registry, empty disables registration")
	registryDelay     = flag.Duration("registry-delay", time.Minute, "determines how long the controller waits before registering at the OIM registry")
)

func main() {
	flag.Parse()
	closer, err := oimcommon.InitTracer("OIM Controller")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize tracer: %s\n", err)
		os.Exit(1)
	}
	defer closer.Close()

	options := []oimcontroller.Option{
		oimcontroller.WithControllerID(*controllerID),
		oimcontroller.WithSPDK(*spdk),
		oimcontroller.WithVHostController(*vhost),
		oimcontroller.WithVHostDev(*vhostDev),
		oimcontroller.WithControllerAddress(*controllerAddress),
		oimcontroller.WithRegistry(*registry),
		oimcontroller.WithRegistryDelay(*registryDelay),
	}
	controller, err := oimcontroller.New(options...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize server: %s\n", err)
		os.Exit(1)
	}
	server, service := oimcontroller.Server(*endpoint, controller)
	if err := server.Run(service); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to run server: %s\n", err)
		os.Exit(1)
	}
}
