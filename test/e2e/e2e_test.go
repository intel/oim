/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"flag"
	"fmt"
	"log"
	"os"
	"testing"

	"k8s.io/kubernetes/test/e2e/framework"

	. "github.com/onsi/ginkgo"

	// test sources
	_ "github.com/intel/oim/test/e2e/storage"
)

func init() {
	log.SetOutput(GinkgoWriter)

	framework.ViperizeFlags()
	// This check probably should be in the Kubernetes framework itself.
	args := flag.Args()
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "unhandled extra command line arguments: %v\n", args)
		os.Exit(1)
	}
}

func TestE2E(t *testing.T) {
	RunE2ETests(t)
}
