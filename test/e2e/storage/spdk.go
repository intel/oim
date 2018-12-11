/*
Copyright 2018 The Kubernetes Authors.

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

package storage

import (
	"context"
	"strings"

	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/intel/oim/pkg/spdk"
	"github.com/intel/oim/test/pkg/qemu"
	testspdk "github.com/intel/oim/test/pkg/spdk"

	// nolint: golint
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("SPDK", func() {
	f := framework.NewDefaultFramework("spdk")
	var (
		ctx = context.Background()
	)

	BeforeEach(func() {
		if testspdk.SPDK == nil {
			Skip("No SPDK vhost.")
		}
	})

	// This is covered by the full E2E volume test with Ceph, but
	// kept here because it might be useful to change it back from
	// XIt() to It() and run it manually via
	// -ginkgo.focus=should.create.RBD.BDev
	XIt("should create RBD BDev", func() {
		// TODO: use f.UniqueName once we have a framework which supports it (https://github.com/kubernetes/kubernetes/pull/69868)
		// and disable namespace creation

		image := "e2e-test-spdk-image-" + f.Namespace.Name
		pool := "rbd"
		out, err := qemu.VM.SSH("rbd", "create", image, "--size", "1", "--pool", pool)
		Expect(err).NotTo(HaveOccurred(), "command output: %s", out)
		// Trash the image here, because that always works even when it is still in use.
		defer qemu.VM.SSH("rbd", "trash", "move", image)

		// Trash whatever was left over previously.
		out, err = qemu.VM.SSH("rbd", "trash", "purge")
		Expect(err).NotTo(HaveOccurred(), "command output: %s", out)

		// Get the current "kubernetes" key.
		key, err := qemu.VM.SSH("grep", "key", "/etc/ceph/ceph.client.kubernetes.keyring")
		Expect(err).NotTo(HaveOccurred(), "command output: %s", out)
		parts := strings.Split(key, " = ")
		key = parts[1]

		request := spdk.ConstructRBDBDevArgs{
			BlockSize: 512,
			Name:      "e2e-test-spdk-bdev",
			PoolName:  pool,
			UserID:    "kubernetes",
			RBDName:   image,
			Config: map[string]string{
				"mon_host": "192.168.7.2:6789",
				"key":      key,
			},
		}
		_, err = spdk.ConstructRBDBDev(ctx, testspdk.SPDK, request)
		Expect(err).NotTo(HaveOccurred(), "ConstructRBDBDev for %+v", request)
	})
})
