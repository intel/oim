/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package storage

import (
	"context"
	"flag"
	"io/ioutil"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"

	"github.com/intel/oim/test/e2e/framework"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/api/legacyscheme"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-controller"
	"github.com/intel/oim/pkg/oim-registry"
	"github.com/intel/oim/test/pkg/spdk"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "localhost"
	}
	return hostname
}

var (
	oimImageRegistry = flag.String("oimImageRegistry", "192.168.7.1:5000", "overrides the default repository used for the OIM CSI driver image (must be reachable from inside QEMU)")
)

func csiOIMMalloc(
	client clientset.Interface,
	config framework.VolumeTestConfig,
	teardown bool,
	f *framework.Framework,
	sa *v1.ServiceAccount,
	registryAddress, controllerID string,
) {
	var err error
	daemonsetClient := client.AppsV1().DaemonSets(config.Namespace)

	var daemonset appsv1.DaemonSet
	EntityFromManifestOrDie("deploy/kubernetes/malloc/malloc-daemonset.yaml", &daemonset)
	daemonset.ObjectMeta.Namespace = config.Namespace
	spec := &daemonset.Spec.Template.Spec
	spec.ServiceAccountName = sa.GetName()
	driverContainer := setContainerImage(spec.Containers, "oim-csi-driver", *oimImageRegistry+"/oim-csi-driver:canary")
	Expect(driverContainer).NotTo(BeNil())
	driverContainer.Args = append(spec.Containers[0].Args, "--oim-registry-address="+registryAddress)
	// TODO: find OIM controller via host name
	driverContainer.Args = append(spec.Containers[0].Args, "--controller-id="+controllerID)
	Expect(setContainerImage(spec.Containers, "external-provisioner", csiContainerImage("csi-provisioner"))).NotTo(BeNil())
	Expect(setContainerImage(spec.Containers, "driver-registrar", csiContainerImage("driver-registrar"))).NotTo(BeNil())
	Expect(setContainerImage(spec.Containers, "external-attacher", csiContainerImage("csi-attacher"))).NotTo(BeNil())

	err = daemonsetClient.Delete(daemonset.ObjectMeta.Name, nil)
	if err != nil && !apierrs.IsNotFound(err) {
		framework.ExpectNoError(err, "Failed to delete daemonset %s/%s: %v",
			daemonset.GetNamespace(), daemonset.GetName(), err)
	}

	if teardown {
		return
	}

	_, err = daemonsetClient.Create(&daemonset)
	framework.ExpectNoError(err, "Failed to create daemonset %s/%s: %v",
		daemonset.GetNamespace(), daemonset.GetName(), err)
}

func setContainerImage(containers []v1.Container, name, image string) *v1.Container {
	for i, _ := range containers {
		container := &containers[i]
		if container.Name == name {
			container.Image = image
			return container
		}
	}
	return nil
}

type OIMControlPlane struct {
	registryServer, controllerServer *oimcommon.NonBlockingGRPCServer
	controller                       *oimcontroller.Controller
	tmpDir                           string
	ctx                              context.Context
	cancel                           context.CancelFunc

	controllerID    string
	registryAddress string
}

// TODO: test binaries instead or in addition?
func (op *OIMControlPlane) StartOIMControlPlane(ctx context.Context) {
	var err error

	if spdk.SPDK == nil {
		Skip("No SPDK vhost.")
	}

	op.ctx, op.cancel = context.WithCancel(ctx)

	// Spin up registry on the host. We
	// intentionally use the hostname here instead
	// of localhost, because then the resulting
	// address has one external IP address.
	// The assumptions are that:
	// - the hostname can be resolved
	// - the resulting IP address is different
	//   from the network inside QEMU and thus
	//   can be reached via the QEMU NAT from inside
	//   the virtual machine
	By("starting OIM registry")
	registry, err := oimregistry.New()
	Expect(err).NotTo(HaveOccurred())
	hostname, err := os.Hostname()
	Expect(err).NotTo(HaveOccurred())
	rs, registryService := oimregistry.Server("tcp4://"+hostname+":0", registry)
	op.registryServer = rs
	err = op.registryServer.Start(ctx, registryService)
	Expect(err).NotTo(HaveOccurred())
	addr := op.registryServer.Addr()
	Expect(addr).NotTo(BeNil())
	// No tcp4:/// prefix. It causes gRPC to block?!
	op.registryAddress = addr.String()

	By("starting OIM controller")
	op.controllerID = "oim-e2e-controller"
	op.tmpDir, err = ioutil.TempDir("", "oim-e2e-test")
	Expect(err).NotTo(HaveOccurred())
	controllerAddress := "unix:///" + op.tmpDir + "/controller.sock"
	op.controller, err = oimcontroller.New(
		oimcontroller.WithRegistry(op.registryAddress),
		oimcontroller.WithControllerID(op.controllerID),
		oimcontroller.WithControllerAddress(controllerAddress),
		oimcontroller.WithVHostController(spdk.VHost),
		oimcontroller.WithVHostDev(spdk.VHostDev),
		oimcontroller.WithSPDK(spdk.SPDKPath),
	)
	Expect(err).NotTo(HaveOccurred())
	cs, controllerService := oimcontroller.Server(controllerAddress, op.controller)
	op.controllerServer = cs
	err = op.controllerServer.Start(ctx, controllerService)
	Expect(err).NotTo(HaveOccurred())
	err = op.controller.Start()
	Expect(err).NotTo(HaveOccurred())
}

func (op *OIMControlPlane) StopOIMControlPlane(ctx context.Context) {
	By("stopping OIM services")

	if op.cancel != nil {
		op.cancel()
	}
	if op.registryServer != nil {
		op.registryServer.ForceStop(ctx)
		op.registryServer.Wait(ctx)
	}
	if op.controllerServer != nil {
		op.controllerServer.ForceStop(ctx)
		op.controllerServer.Wait(ctx)
	}
	if op.controller != nil {
		op.controller.Stop()
	}
	if op.tmpDir != "" {
		os.RemoveAll(op.tmpDir)
	}
}

// EntityFromManifest reads a Kubernetes .yaml file and populates the
// given entity with its content.
func EntityFromManifest(fileName string, target runtime.Object) error {
	// Ultimately this should use the "testfiles" package from https://github.com/kubernetes/kubernetes/pull/69105
	// For now we just approximate it.
	data, err := ioutil.ReadFile(fileName)
	if err != nil && os.IsNotExist(err) {
		// Probably started by Ginkgo in test/e2e. Try two levels up.
		data, err = ioutil.ReadFile("../../" + fileName)
		if err != nil {
			return err
		}
	}

	json, err := utilyaml.ToJSON(data)
	if err != nil {
		return err
	}

	err = runtime.DecodeInto(legacyscheme.Codecs.UniversalDecoder(), json, target)
	return err
}

func EntityFromManifestOrDie(fileName string, target runtime.Object) {
	err := EntityFromManifest(fileName, target)
	framework.ExpectNoError(err, "Failed to load manifest: %q", fileName)
}
