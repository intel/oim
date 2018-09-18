/*
Copyright 2018 Intel Coporation.

SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/grpc"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/spec/oim/v0"
)

var (
	endpoint = flag.String("registry", "", "the gRPC endpoint of the OIM registry (for example, dns:///localhost:8999)")
	_        = log.InitSimpleFlags()

	// Quick-and-dirty bool flags for triggering operations. What we want instead is
	// probably something like a Cobra-based command line tool. We also need to consider
	// keys which contain the = sign: right now, the command line parsing does not support those.
	get = flag.Bool("get", false, "prints the current registry as <key>=<value> pairs to stdout")
	set = flag.String("set", "", "sets or updates (for <key>=<value>) or removes (for <key>= or just <key>) a registry entry")
)

func main() {
	ctx := context.Background()

	flag.Parse()

	config := log.NewSimpleConfig()
	config.Output = os.Stderr
	logger := log.NewSimpleLogger(config)
	log.Set(logger)

	if *endpoint == "" {
		logger.Fatal("-registry must be set")
	}

	// TODO: secure connection
	opts := oimcommon.ChooseDialOpts(*endpoint, grpc.WithInsecure())
	conn, err := grpc.DialContext(ctx, *endpoint, opts...)
	if err != nil {
		logger.Fatalw("connecting to OIM registry", "error", err)
	}
	defer conn.Close()

	registry := oim.NewRegistryClient(conn)

	if *set != "" {
		values := strings.SplitN(*set, "=", 2)
		controllerID := values[0]
		var controllerAddr string
		if len(values) == 2 {
			controllerAddr = values[1]
		}
		_, err := registry.RegisterController(ctx, &oim.RegisterControllerRequest{
			ControllerId: controllerID,
			Address:      controllerAddr,
		})
		if err != nil {
			logger.Fatalw("setting a registry entry", "error", err, "key", controllerID, "value", controllerAddr)
		}
	}
	if *get {
		reply, err := registry.GetControllers(ctx, &oim.GetControllerRequest{})
		if err != nil {
			logger.Fatalw("getting registry entries", "error", err)
		}
		sort.SliceStable(reply.Entries, func(i, j int) bool {
			return strings.Compare(reply.Entries[i].ControllerId, reply.Entries[j].ControllerId) < 0
		})
		for _, entry := range reply.Entries {
			fmt.Printf("%s=%s\n", entry.ControllerId, entry.Address)
		}
	}
}
