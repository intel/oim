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

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-csi-driver"
)

func init() {
	flag.Set("logtostderr", "true")
}

var (
	endpoint           = flag.String("endpoint", "unix:///tmp/csi.sock", "CSI endpoint")
	driverName         = flag.String("drivername", "oim-csi-driver", "name of the driver")
	nodeID             = flag.String("nodeid", "", "node id")
	spdkSocket         = flag.String("spdk-socket", "", "SPDK VHost socket path. If set, then the driver will controll that SPDK instance directly.")
	oimRegistryAddress = flag.String("oim-registry-address", "", "OIM registry address in the format expected by grpc.Dial. If set, then the driver will use a OIM controller via the registry instead of a local SPDK daemon.")
	controllerID       = flag.String("controller-id", "", "The ID under which the OIM controller can be found in the registry.")
)

func main() {
	flag.Parse()
	closer, err := oimcommon.InitTracer(*driverName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize tracer: %s\n", err)
		os.Exit(1)
	}
	defer closer.Close()

	options := []oimcsidriver.Option{
		oimcsidriver.WithDriverName(*driverName),
		oimcsidriver.WithCSIEndpoint(*endpoint),
		oimcsidriver.WithNodeID(*nodeID),
		oimcsidriver.WithVHostEndpoint(*spdkSocket),
		oimcsidriver.WithOIMRegistryAddress(*oimRegistryAddress),
		oimcsidriver.WithOIMControllerID(*controllerID),
	}
	driver, err := oimcsidriver.New(options...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize driver: %s\n", err)
		os.Exit(1)
	}
	driver.Run()
}
