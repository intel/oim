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
	get   = flag.Bool("get", false, "retrieve values from the registry as <key>=<value> pairs to stdout")
	set   = flag.Bool("set", false, "sets or updates a registry value, deletes it when value is empty")
	path  = flag.String("path", "", "the complete path of a value (set, delete, get of single value) or a path prefix (get multiple values)")
	value = flag.String("value", "", "the value to set or update")
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

	// sanitize path
	elements, err := oimcommon.SplitRegistryPath(*path)
	if err != nil {
		logger.Fatalw("path", *path, "error", err)
	}
	key := oimcommon.JoinRegistryPath(elements)

	if *set {
		if key == "" {
			logger.Fatal("key required")
		}
		_, err := registry.SetValue(ctx, &oim.SetValueRequest{
			Value: &oim.Value{
				Path:  key,
				Value: *value,
			},
		})
		if err != nil {
			logger.Fatalw("setting a registry value", "error", err, "path", key, "value", *value)
		}
	} else if *get {
		if *value != "" {
			logger.Fatalw("value not allowed for --get", "value", *value)
		}
		reply, err := registry.GetValues(ctx, &oim.GetValuesRequest{
			Path: key,
		})
		if err != nil {
			logger.Fatalw("getting registry values", "error", err)
		}
		sort.SliceStable(reply.Values, func(i, j int) bool {
			return strings.Compare(reply.Values[i].Path, reply.Values[j].Path) < 0
		})
		for _, entry := range reply.Values {
			fmt.Printf("%s=%s\n", entry.Path, entry.Value)
		}
	} else {
		logger.Fatal("either --get or --set must be chosen")
	}
}
