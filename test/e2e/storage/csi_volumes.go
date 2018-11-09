/*
Copyright 2017 The Kubernetes Authors.

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
	"fmt"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/podlogs"
	"k8s.io/kubernetes/test/e2e/storage/testpatterns"
	"k8s.io/kubernetes/test/e2e/storage/testsuites"
	"k8s.io/kubernetes/test/e2e/storage/utils"

	"github.com/intel/oim/test/pkg/spdk"

	// nolint: golint
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func csiTunePattern(patterns []testpatterns.TestPattern) []testpatterns.TestPattern {
	tunedPatterns := []testpatterns.TestPattern{}

	for _, pattern := range patterns {
		// Skip inline volume and pre-provsioned PV tests for csi drivers
		if pattern.VolType == testpatterns.InlineVolume || pattern.VolType == testpatterns.PreprovisionedPV {
			continue
		}
		tunedPatterns = append(tunedPatterns, pattern)
	}

	return tunedPatterns
}

var _ = Describe("OIM Volumes", func() {
	f := framework.NewDefaultFramework("oim")

	var (
		cs           clientset.Interface
		ns           *v1.Namespace
		ctx          = context.Background()
		controlPlane OIMControlPlane
		destructors  []func()
	)

	BeforeEach(func() {
		if spdk.SPDK == nil {
			Skip("No SPDK vhost.")
		}

		cs = f.ClientSet
		ns = f.Namespace

		controlPlane.StartOIMControlPlane(ctx)
		var cleanup framework.CleanupActionHandle
		destructor := func() {
			if cleanup == nil {
				return
			}
			framework.RemoveCleanupAction(cleanup)
			controlPlane.StopOIMControlPlane(ctx)
		}
		cleanup = framework.AddCleanupAction(destructor)
		destructors = append(destructors, destructor)

		to := podlogs.LogOutput{
			StatusWriter: GinkgoWriter,
			LogWriter:    GinkgoWriter,
		}
		if err := podlogs.CopyAllLogs(controlPlane.ctx, cs, ns.Name, to); err != nil {
			framework.Logf("copying logs impossible: %s", err)
		}
		if err := podlogs.WatchPods(controlPlane.ctx, cs, ns.Name, GinkgoWriter); err != nil {
			framework.Logf("watching pods impossible: %s", err)
		}
	})

	AfterEach(func() {
		for _, destructor := range destructors {
			destructor()
		}
	})

	// List of testDrivers to be executed in below loop
	var csiTestDrivers = []func() testsuites.TestDriver{
		// OIM CSI with Malloc BDEV
		func() testsuites.TestDriver {
			return &manifestDriver{
				controlPlane: &controlPlane,
				driverInfo: testsuites.DriverInfo{
					Name:        "oim-malloc",
					MaxFileSize: testpatterns.FileSizeMedium,
					SupportedFsType: sets.NewString(
						"", // Default fsType
					),
					Capabilities: map[testsuites.Capability]bool{
						testsuites.CapPersistence: true,
						testsuites.CapFsGroup:     true,
						testsuites.CapExec:        true,
					},

					Config: testsuites.TestConfig{
						Framework: f,
						Prefix:    "oim-malloc",
						// Ensure that we land on the right node.
						ClientNodeName: "host-0",
					},
				},
				manifests: []string{
					os.ExpandEnv("${TEST_WORK}/ca/secret.yaml"),
					"deploy/kubernetes/malloc/malloc-rbac.yaml",
					"deploy/kubernetes/malloc/malloc-daemonset.yaml",
				},
				scManifest: "deploy/kubernetes/malloc/malloc-storageclass.yaml",
				// Enable renaming of the driver.
				patchOptions: utils.PatchCSIOptions{
					OldDriverName:            "oim-malloc",
					NewDriverName:            "oim-malloc-", // f.UniqueName must be added later
					DriverContainerName:      "oim-csi-driver",
					ProvisionerContainerName: "external-provisioner",
				},
				claimSize: "1Mi",
			}
		},

		// OIM CSI with ceph-csi
		func() testsuites.TestDriver {
			return &manifestDriver{
				controlPlane: &controlPlane,
				driverInfo: testsuites.DriverInfo{
					Name:        "oim-rbd",
					MaxFileSize: testpatterns.FileSizeMedium,
					SupportedFsType: sets.NewString(
						"", // Default fsType
					),
					Capabilities: map[testsuites.Capability]bool{
						testsuites.CapPersistence: true,
						testsuites.CapFsGroup:     true,
						testsuites.CapExec:        true,
					},

					Config: testsuites.TestConfig{
						Framework: f,
						Prefix:    "oim-rbd",
					},
				},
				manifests: []string{
					os.ExpandEnv("${TEST_WORK}/ca/secret.yaml"),
					"deploy/kubernetes/ceph-csi/rbd-rbac.yaml",
					"deploy/kubernetes/ceph-csi/rbd-node.yaml",
					"deploy/kubernetes/ceph-csi/oim-node.yaml",
					"deploy/kubernetes/ceph-csi/rbd-statefulset.yaml",
				},
				scManifest: "deploy/kubernetes/ceph-csi/rbd-storageclass.yaml",
				// TODO: Enable renaming of the driver.
				// patchOptions: utils.PatchCSIOptions{
				// 	OldDriverName:            "oim-rbd",
				// 	NewDriverName:            "oim-rbd-", // f.UniqueName must be added later
				// 	DriverContainerName:      "oim-csi-driver", more than one!
				// 	ProvisionerContainerName: "external-provisioner",
				// },
				// See https://github.com/ceph/ceph-csi/issues/85
				claimSize: "1Gi",
			}
		},
	}

	// List of testSuites to be executed in below loop
	var csiTestSuites = []func() testsuites.TestSuite{
		// TODO: investigate how useful these tests are and enable them.
		// testsuites.InitVolumesTestSuite,
		// testsuites.InitVolumeIOTestSuite,
		// testsuites.InitVolumeModeTestSuite,
		// testsuites.InitSubPathTestSuite,
		testsuites.InitProvisioningTestSuite,
	}

	for _, initDriver := range csiTestDrivers {
		curDriver := initDriver()
		Context(testsuites.GetDriverNameWithFeatureTags(curDriver), func() {
			driver := curDriver

			BeforeEach(func() {
				// setupDriver
				driver.CreateDriver()
			})

			AfterEach(func() {
				// Cleanup driver
				driver.CleanupDriver()
			})

			testsuites.RunTestSuite(f, driver, csiTestSuites, csiTunePattern)
		})
	}
})

type manifestDriver struct {
	controlPlane *OIMControlPlane
	driverInfo   testsuites.DriverInfo
	patchOptions utils.PatchCSIOptions
	manifests    []string
	scManifest   string
	claimSize    string
	cleanup      func()
}

var _ testsuites.TestDriver = &manifestDriver{}
var _ testsuites.DynamicPVTestDriver = &manifestDriver{}

func (m *manifestDriver) GetDriverInfo() *testsuites.DriverInfo {
	return &m.driverInfo
}

func (*manifestDriver) SkipUnsupportedTest(testpatterns.TestPattern) {
}

func (m *manifestDriver) GetDynamicProvisionStorageClass(fsType string) *storagev1.StorageClass {
	f := m.driverInfo.Config.Framework

	items, err := f.LoadFromManifests(m.scManifest)
	Expect(err).NotTo(HaveOccurred())
	Expect(len(items)).To(Equal(1), "exactly one item from %s", m.scManifest)

	err = f.PatchItems(items...)
	Expect(err).NotTo(HaveOccurred())
	err = utils.PatchCSIDeployment(f, m.finalPatchOptions(), items[0])
	Expect(err).NotTo(HaveOccurred())

	sc, ok := items[0].(*storagev1.StorageClass)
	Expect(ok).To(BeTrue(), "storage class from %s", m.scManifest)
	return sc
}

func (m *manifestDriver) GetClaimSize() string {
	return m.claimSize
}

func (m *manifestDriver) CreateDriver() {
	By(fmt.Sprintf("deploying %s driver", m.driverInfo.Name))
	f := m.driverInfo.Config.Framework

	// TODO (?): the storage.csi.image.version and storage.csi.image.registry
	// settings are ignored for this test. We could patch the image definitions.
	cleanup, err := f.CreateFromManifests(func(item interface{}) error {
		m.patchOIM(item)
		return utils.PatchCSIDeployment(f, m.finalPatchOptions(), item)
	},
		m.manifests...,
	)
	m.cleanup = cleanup
	if err != nil {
		framework.Failf("deploying %s driver: %v", m.driverInfo.Name, err)
	}
}

func (m *manifestDriver) CleanupDriver() {
	if m.cleanup != nil {
		By(fmt.Sprintf("uninstalling %s driver", m.driverInfo.Name))
		m.cleanup()
	}
}

func (m *manifestDriver) patchOIM(item interface{}) {
	switch item := item.(type) {
	case *appsv1.DaemonSet:
		containers := &item.Spec.Template.Spec.Containers
		for i := range *containers {
			container := &(*containers)[i]
			for e := range container.Args {
				// Replace @OIM_REGISTRY_ADDRESS@ in the DaemonSet.
				container.Args[e] = strings.Replace(container.Args[e], "@OIM_REGISTRY_ADDRESS@", m.controlPlane.registryAddress, 1)
			}
		}
	}
}

func (m *manifestDriver) finalPatchOptions() utils.PatchCSIOptions {
	o := m.patchOptions
	// Unique name not available yet when configuring the driver.
	if strings.HasSuffix(o.NewDriverName, "-") {
		o.NewDriverName += m.driverInfo.Config.Framework.UniqueName
	}
	return o
}
