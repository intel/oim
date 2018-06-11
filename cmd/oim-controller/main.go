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
	"github.com/intel/oim/pkg/oim-controller"
	"github.com/intel/oim/pkg/spec/oim/v0"
)

func init() {
	flag.Set("logtostderr", "true")
}

var (
	endpoint   = flag.String("endpoint", "unix:///tmp/controller.sock", "OIM controller endpoint")
	spdk       = flag.String("spdk", "/var/tmp/vhost.sock", "SPDK VHost RPC socket path")
	vhost      = flag.String("vhost-scsi-controller", "vhost.0", "SPDK VirtIO SCSI controller name")
	hardwareID = flag.String("hardwareid", "", "unique id for the controlled hardware")
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
		oimcontroller.OptionHardwareID(*hardwareID),
		oimcontroller.OptionSPDK(*spdk),
		oimcontroller.OptionVHostController(*vhost),
	}
	controller, err := oimcontroller.New(options...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize server: %s\n", err)
		os.Exit(1)
	}
	server := oimcommon.NonBlockingGRPCServer{
		Endpoint: *endpoint,
	}
	service := func(s *grpc.Server) {
		oim.RegisterControllerServer(s, controller)
	}
	if err := server.Run(service); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to run server: %s\n", err)
		os.Exit(1)
	}
}
