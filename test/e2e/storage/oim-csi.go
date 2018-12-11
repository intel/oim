/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package storage

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/kubernetes-csi/csi-test/pkg/sanity"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/test/pkg/qemu"

	// nolint: golint
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// Runs tests with SPDK outside of QEMU and OIM CSI driver inside.
//
// The corresponding test for local SPDK mode is in
// pkg/oim-csi-driver/oim-driver_test.go.

var _ = Describe("OIM CSI driver", func() {
	// The test is part of tests/e2e because it can use the same VM
	// instance in parallel to the long-running Kubernetes test.
	// It no longer uses Kubernetes to deploy the OIM CSI driver
	// because that turned out to be pretty slow. Instead the binary
	// gets installed via SSH.

	var (
		driverBinary = os.Getenv("TEST_OIM_CSI_DRIVER_BINARY")
		targetTmp    string
		localTmp     string
		config       = sanity.Config{
			TestVolumeSize: 1 * 1024 * 1024,
		}
		port         io.Closer
		controlPlane OIMControlPlane
		ctx          = context.Background()
	)

	BeforeEach(func() {
		var err error

		if driverBinary == "" {
			Skip("TEST_OIM_CSI_DRIVER_BINARY not set")
		}

		controlPlane.StartOIMControlPlane(ctx)
		localTmp, err = ioutil.TempDir("", "oim-csi-sanity")
		Expect(err).NotTo(HaveOccurred())

		By("deploying OIM CSI driver")
		// This uses a temp directory on the target and
		// locally, with Unix domain sockets inside
		// those. This way we we don't need to worry about
		// conflicting use of IP ports.
		targetTmp, err = qemu.VM.SSH("mktemp", "-d", "-p", "/var/tmp")
		Expect(err).NotTo(HaveOccurred())
		targetTmp = strings.Trim(targetTmp, "\n")
		install := func(from, to string) {
			// can be ignored: warning: Potential file inclusion via variable
			f, err := os.Open(from) // nolint: gosec
			defer f.Close()
			Expect(err).NotTo(HaveOccurred())
			err = qemu.VM.Install(to, f, 0555)
			Expect(err).NotTo(HaveOccurred())
		}
		driverPath := filepath.Join(targetTmp, "oim-csi-driver")
		install(driverBinary, driverPath)
		caPath := filepath.Join(targetTmp, "ca.crt")
		install(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"), caPath)
		keyPath := filepath.Join(targetTmp, "host.host-0.key")
		install(os.ExpandEnv("${TEST_WORK}/ca/host.host-0.key"), keyPath)
		crtPath := filepath.Join(targetTmp, "host.host-0.crt")
		install(os.ExpandEnv("${TEST_WORK}/ca/host.host-0.crt"), crtPath)
		csiSock := filepath.Join(targetTmp, "csi.sock")
		csiEndpoint := "unix://" + csiSock
		localSock := filepath.Join(localTmp, "csi.sock")
		config.Address = "unix://" + localSock
		config.TargetPath = filepath.Join(targetTmp, "target")
		config.StagingPath = filepath.Join(targetTmp, "staging")
		p, _, err := qemu.VM.ForwardPort(log.L(),
			localSock, csiSock,
			driverPath,
			"--log.level=DEBUG",
			"--endpoint", csiEndpoint,
			"--nodeid=host-0",
			"--ca="+caPath,
			"--key="+keyPath,
			"--oim-registry-address", controlPlane.registryAddress,
			"--controller-id", controlPlane.controllerID,
		)
		Expect(err).NotTo(HaveOccurred())
		port = p
	})

	AfterEach(func() {
		var err error

		By("uninstalling CSI OIM driver")
		err = port.Close()
		Expect(err).NotTo(HaveOccurred())
		err = os.RemoveAll(localTmp)
		Expect(err).NotTo(HaveOccurred())
		_, err = qemu.VM.SSH("rm", "-rf", targetTmp)
		Expect(err).NotTo(HaveOccurred())

		controlPlane.StopOIMControlPlane(ctx)
	})

	Describe("sanity", func() {
		sanity.GinkgoTest(&config)
	})
})
