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
	"github.com/intel/oim/pkg/oim-controller"
)

func init() {
	flag.Set("logtostderr", "true")
}

var (
	endpoint     = flag.String("endpoint", "unix:///tmp/controller.sock", "OIM controller endpoint")
	spdk         = flag.String("spdk", "/var/tmp/vhost.sock", "SPDK VHost RPC socket path")
	vhost        = flag.String("vhost-scsi-controller", "vhost.0", "SPDK VirtIO SCSI controller name")
	controllerID = flag.String("controllerid", "", "unique id for this controller instance")
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
