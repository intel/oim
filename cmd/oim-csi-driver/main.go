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
	endpoint   = flag.String("endpoint", "unix:///tmp/csi.sock", "CSI endpoint")
	driverName = flag.String("drivername", "oim-csi-driver", "name of the driver")
	nodeID     = flag.String("nodeid", "", "node id")
)

func main() {
	flag.Parse()
	closer, err := oimcommon.InitTracer(*driverName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize tracer: %s\n", err)
		os.Exit(1)
	}
	defer closer.Close()

	options := []oimcsidriver.DriverOption{
		oimcsidriver.OptionDriverName(*driverName),
		oimcsidriver.OptionCSIEndpoint(*endpoint),
		oimcsidriver.OptionNodeID(*nodeID),
	}
	driver, err := oimcsidriver.GetOIMDriver(options...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize driver: %s\n", err)
		os.Exit(1)
	}
	driver.Run()
}
