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

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/intel/oim/test/e2e/framework"
	clientset "k8s.io/client-go/kubernetes"

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
	oimImageRegistry = flag.String("oimImageRegistry", getHostname()+":5000", "overrides the default repository used for the OIM CSI driver image (must be reachable from inside QEMU)")
)

func csiOIMPod(
	client clientset.Interface,
	config framework.VolumeTestConfig,
	teardown bool,
	f *framework.Framework,
	sa *v1.ServiceAccount,
	registryAddress, controllerID string,
) *v1.Pod {
	podClient := client.CoreV1().Pods(config.Namespace)

	priv := true
	mountPropagation := v1.MountPropagationBidirectional
	hostPathType := v1.HostPathDirectoryOrCreate
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Prefix + "-pod",
			Namespace: config.Namespace,
			Labels: map[string]string{
				"app": "oim-csi-driver",
			},
		},
		Spec: v1.PodSpec{
			ServiceAccountName: sa.GetName(),
			NodeName:           config.ServerNodeName,
			RestartPolicy:      v1.RestartPolicyNever,
			Containers: []v1.Container{
				{
					Name:            "oim-csi-driver",
					Image:           *oimImageRegistry + "/oim-csi-driver:canary",
					ImagePullPolicy: v1.PullAlways,
					SecurityContext: &v1.SecurityContext{
						Privileged: &priv,
					},
					Args: []string{
						"--v=5", // TODO: get rid of glog
						"--log.level=DEBUG",
						"--endpoint=$(CSI_ENDPOINT)",
						"--nodeid=$(KUBE_NODE_NAME)",
						"--oim-registry-address=$(OIM_REGISTRY_ADDRESS)",
						"--controller-id=$(OIM_CONTROLLER_ID)",
					},
					Env: []v1.EnvVar{
						{
							Name:  "CSI_ENDPOINT",
							Value: "unix://" + "/csi/csi.sock",
						},
						{
							Name: "KUBE_NODE_NAME",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "spec.nodeName",
								},
							},
						},
						{
							Name:  "OIM_REGISTRY_ADDRESS",
							Value: registryAddress,
						},
						{
							Name:  "OIM_CONTROLLER_ID",
							Value: controllerID,
						},
					},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      "socket-dir",
							MountPath: "/csi",
						},
						{
							Name:             "mountpoint-dir",
							MountPath:        "/var/lib/kubelet/pods",
							MountPropagation: &mountPropagation,
						},
					},
				},
				{
					Name:            "external-provisioner",
					Image:           csiContainerImage("csi-provisioner"),
					ImagePullPolicy: v1.PullIfNotPresent,
					Args: []string{
						"--v=5",
						"--provisioner=oim-csi-driver",
						"--csi-address=/csi/csi.sock",
					},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      "socket-dir",
							MountPath: "/csi",
						},
					},
				},
				{
					Name:            "driver-registrar",
					Image:           csiContainerImage("driver-registrar"),
					ImagePullPolicy: v1.PullIfNotPresent,
					Args: []string{
						"--v=5",
						"--csi-address=/csi/csi.sock",
					},
					Env: []v1.EnvVar{
						{
							Name: "KUBE_NODE_NAME",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "spec.nodeName",
								},
							},
						},
					},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      "socket-dir",
							MountPath: "/csi",
						},
					},
				},
				{
					Name:            "external-attacher",
					Image:           csiContainerImage("csi-attacher"),
					ImagePullPolicy: v1.PullIfNotPresent,
					Args: []string{
						"--v=5",
						"--csi-address=$(ADDRESS)",
					},
					Env: []v1.EnvVar{
						{
							Name:  "ADDRESS",
							Value: "/csi/csi.sock",
						},
					},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      "socket-dir",
							MountPath: "/csi",
						},
					},
				},
			},
			Volumes: []v1.Volume{
				{
					Name: "socket-dir",
					VolumeSource: v1.VolumeSource{
						HostPath: &v1.HostPathVolumeSource{
							Path: "/var/lib/kubelet/plugins/oim-csi-driver",
							Type: &hostPathType,
						},
					},
				},
				{
					Name: "mountpoint-dir",
					VolumeSource: v1.VolumeSource{
						HostPath: &v1.HostPathVolumeSource{
							Path: "/var/lib/kubelet/pods",
							Type: &hostPathType,
						},
					},
				},
			},
		},
	}

	err := framework.DeletePodWithWait(f, client, pod)
	framework.ExpectNoError(err, "Failed to delete pod %s/%s: %v",
		pod.GetNamespace(), pod.GetName(), err)

	if teardown {
		return nil
	}

	ret, err := podClient.Create(pod)
	if err != nil {
		framework.ExpectNoError(err, "Failed to create %q pod: %v", pod.GetName(), err)
	}

	// Wait for pod to come up
	framework.ExpectNoError(framework.WaitForPodRunningInNamespace(client, ret))
	return ret
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
