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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/storage/utils"

	"github.com/intel/oim/test/pkg/spdk"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = utils.SIGDescribe("OIM Volumes", func() {
	f := framework.NewDefaultFramework("oim")

	var (
		cs           clientset.Interface
		ns           *v1.Namespace
		config       framework.VolumeTestConfig
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
		config = framework.VolumeTestConfig{
			Namespace:         ns.GetName(),
			Prefix:            "oim",
			NodeSelector:      map[string]string{"intel.com/oim": "1"},
			WaitForCompletion: true,
		}

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

		// TODO
		// podlogs.CopyAllLogs(controlPlane.ctx, cs, ns.Name, GinkgoWriter)
		// podlogs.WatchPods(controlPlane.ctx, cs, GinkgoWriter)
	})

	AfterEach(func() {
		for _, destructor := range destructors {
			destructor()
		}
	})

	patchOIM := func(object interface{}) error {
		switch object := object.(type) {
		case *appsv1.DaemonSet:
			containers := &object.Spec.Template.Spec.Containers
			for i := range *containers {
				container := &(*containers)[i]
				for e := range container.Args {
					// Replace @OIM_REGISTRY_ADDRESS@ in the DaemonSet.
					container.Args[e] = strings.Replace(container.Args[e], "@OIM_REGISTRY_ADDRESS@", controlPlane.registryAddress, 1)
					// Update --drivername and --provider.
					if strings.HasSuffix(container.Args[e], "=oim-malloc") {
						container.Args[e] = container.Args[e] + "-" + f.UniqueName
					}
				}
			}
			volumes := &object.Spec.Template.Spec.Volumes
			for i := range *volumes {
				volume := &(*volumes)[i]
				// Update path.
				if volume.VolumeSource.HostPath != nil {
					path := &volume.VolumeSource.HostPath.Path
					if strings.HasSuffix(*path, "/oim-malloc") {
						*path = *path + "-" + f.UniqueName
					}
				}
			}
		case *storagev1.StorageClass:
			f.PatchName(&object.Provisioner)
		}

		return nil
	}

	Describe("Sanity CSI plugin test using OIM CSI with Malloc BDev", func() {
		BeforeEach(func() {
			destructor, err := f.CreateFromManifests(
				patchOIM,
				"deploy/kubernetes/malloc/malloc-rbac.yaml",
				"deploy/kubernetes/malloc/malloc-daemonset.yaml",
				"deploy/kubernetes/malloc/malloc-storageclass.yaml",
			)
			destructors = append(destructors, destructor)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should provision storage", func() {
			t := storageClassTest{
				provisioner:  "oim-malloc-" + f.UniqueName,
				parameters:   map[string]string{},
				claimSize:    "1Mi",
				expectedSize: "1Mi",
				nodeSelector: map[string]string{"intel.com/oim": "1"},
			}

			claim := newClaim(t, ns.GetName(), "")
			scName := "oim-malloc-sc-" + f.UniqueName
			claim.Spec.StorageClassName = &scName
			// TODO: check machine state while volume is mounted:
			// a missing UnmapVolume call in nodeserver.go must be detected
			testDynamicProvisioning(t, cs, claim, nil)
		})
	})
})
