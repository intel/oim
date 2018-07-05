/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package storage

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/kubernetes-csi/csi-test/pkg/sanity"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/test/pkg/qemu"

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
	)

	BeforeEach(func() {
		var err error

		if driverBinary == "" {
			Skip("TEST_OIM_CSI_DRIVER_BINARY not set")
		}

		controlPlane.StartOIMControlPlane()
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
		driverPath := filepath.Join(targetTmp, "oim-csi-driver")
		f, err := os.Open(driverBinary)
		Expect(err).NotTo(HaveOccurred())
		err = qemu.VM.Install(driverPath, f, 0555)
		Expect(err).NotTo(HaveOccurred())
		csiSock := filepath.Join(targetTmp, "csi.sock")
		csiEndpoint := "unix://" + csiSock
		localSock := filepath.Join(localTmp, "csi.sock")
		config.Address = "unix://" + localSock
		config.TargetPath = filepath.Join(targetTmp, "target")
		config.StagingPath = filepath.Join(targetTmp, "staging")
		p, _, err := qemu.VM.ForwardPort(oimcommon.WrapWriter(GinkgoWriter),
			localSock, csiSock,
			driverPath,
			"--v=5",
			"--endpoint", csiEndpoint,
			"--nodeid=csi-sanity-node",
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

		controlPlane.StopOIMControlPlane()
	})

	Describe("sanity", func() {
		sanity.GinkgoTest(&config)
	})
})
