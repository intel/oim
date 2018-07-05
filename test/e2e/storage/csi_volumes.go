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
	"io/ioutil"
	"math/rand"
	"os"
	"time"

	"k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/storage/utils"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-controller"
	"github.com/intel/oim/pkg/oim-registry"
	e2eutils "github.com/intel/oim/test/e2e/utils"
	"github.com/intel/oim/test/pkg/qemu"
	"github.com/intel/oim/test/pkg/spdk"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func csiServiceAccount(
	client clientset.Interface,
	config framework.VolumeTestConfig,
	teardown bool,
) *v1.ServiceAccount {
	serviceAccountName := config.Prefix + "-service-account"
	serviceAccountClient := client.CoreV1().ServiceAccounts(config.Namespace)
	sa := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceAccountName,
		},
	}

	serviceAccountClient.Delete(sa.GetName(), &metav1.DeleteOptions{})
	err := wait.Poll(2*time.Second, 10*time.Minute, func() (bool, error) {
		_, err := serviceAccountClient.Get(sa.GetName(), metav1.GetOptions{})
		return apierrs.IsNotFound(err), nil
	})
	framework.ExpectNoError(err, "Timed out waiting for deletion: %v", err)

	if teardown {
		return nil
	}

	ret, err := serviceAccountClient.Create(sa)
	if err != nil {
		framework.ExpectNoError(err, "Failed to create %s service account: %v", sa.GetName(), err)
	}

	return ret
}

func csiClusterRole(
	client clientset.Interface,
	config framework.VolumeTestConfig,
	teardown bool,
) *rbacv1.ClusterRole {
	clusterRoleClient := client.RbacV1().ClusterRoles()
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: config.Prefix + "-cluster-role",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumes"},
				Verbs:     []string{"create", "delete", "get", "list", "watch", "update"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumeclaims"},
				Verbs:     []string{"get", "list", "watch", "update"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"nodes"},
				Verbs:     []string{"get", "list", "watch", "update"},
			},
			{
				APIGroups: []string{"storage.k8s.io"},
				Resources: []string{"volumeattachments"},
				Verbs:     []string{"get", "list", "watch", "update"},
			},
			{
				APIGroups: []string{"storage.k8s.io"},
				Resources: []string{"storageclasses"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	clusterRoleClient.Delete(role.GetName(), &metav1.DeleteOptions{})
	err := wait.Poll(2*time.Second, 10*time.Minute, func() (bool, error) {
		_, err := clusterRoleClient.Get(role.GetName(), metav1.GetOptions{})
		return apierrs.IsNotFound(err), nil
	})
	framework.ExpectNoError(err, "Timed out waiting for deletion: %v", err)

	if teardown {
		return nil
	}

	ret, err := clusterRoleClient.Create(role)
	if err != nil {
		framework.ExpectNoError(err, "Failed to create %s cluster role: %v", role.GetName(), err)
	}

	return ret
}

func csiClusterRoleBinding(
	client clientset.Interface,
	config framework.VolumeTestConfig,
	teardown bool,
	sa *v1.ServiceAccount,
	clusterRole *rbacv1.ClusterRole,
) *rbacv1.ClusterRoleBinding {
	clusterRoleBindingClient := client.RbacV1().ClusterRoleBindings()
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: config.Prefix + "-role-binding",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      sa.GetName(),
				Namespace: sa.GetNamespace(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     clusterRole.GetName(),
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	clusterRoleBindingClient.Delete(binding.GetName(), &metav1.DeleteOptions{})
	err := wait.Poll(2*time.Second, 10*time.Minute, func() (bool, error) {
		_, err := clusterRoleBindingClient.Get(binding.GetName(), metav1.GetOptions{})
		return apierrs.IsNotFound(err), nil
	})
	framework.ExpectNoError(err, "Timed out waiting for deletion: %v", err)

	if teardown {
		return nil
	}

	ret, err := clusterRoleBindingClient.Create(binding)
	if err != nil {
		framework.ExpectNoError(err, "Failed to create %s role binding: %v", binding.GetName(), err)
	}

	return ret
}

var _ = utils.SIGDescribe("CSI Volumes", func() {
	f := framework.NewDefaultFramework("csi-mock-plugin")

	var (
		cs     clientset.Interface
		ns     *v1.Namespace
		node   v1.Node
		config framework.VolumeTestConfig
	)

	BeforeEach(func() {
		cs = f.ClientSet
		ns = f.Namespace
		nodes := framework.GetReadySchedulableNodesOrDie(f.ClientSet)
		node = nodes.Items[rand.Intn(len(nodes.Items))]
		config = framework.VolumeTestConfig{
			Namespace:         ns.Name,
			Prefix:            "csi",
			ClientNodeName:    node.Name,
			ServerNodeName:    node.Name,
			WaitForCompletion: true,
		}
	})

	// Create one of these for each of the drivers to be tested
	// CSI hostPath driver test
	XDescribe("Sanity CSI plugin test using hostPath CSI driver", func() {

		var (
			clusterRole    *rbacv1.ClusterRole
			serviceAccount *v1.ServiceAccount
		)

		BeforeEach(func() {
			By("deploying csi hostpath driver")
			clusterRole = csiClusterRole(cs, config, false)
			serviceAccount = csiServiceAccount(cs, config, false)
			csiClusterRoleBinding(cs, config, false, serviceAccount, clusterRole)
			csiHostPathPod(cs, config, false, f, serviceAccount)
		})

		AfterEach(func() {
			By("uninstalling csi hostpath driver")
			csiHostPathPod(cs, config, true, f, serviceAccount)
			csiClusterRoleBinding(cs, config, true, serviceAccount, clusterRole)
			serviceAccount = csiServiceAccount(cs, config, true)
			clusterRole = csiClusterRole(cs, config, true)
		})

		It("should provision storage with a hostPath CSI driver", func() {
			t := storageClassTest{
				name:         "csi-hostpath",
				provisioner:  "csi-hostpath",
				parameters:   map[string]string{},
				claimSize:    "1Gi",
				expectedSize: "1Gi",
				nodeName:     node.Name,
			}

			claim := newClaim(t, ns.GetName(), "")
			class := newStorageClass(t, ns.GetName(), "")
			claim.Spec.StorageClassName = &class.ObjectMeta.Name
			testDynamicProvisioning(t, cs, claim, class)
		})
	})

	// Create one of these for each of the drivers to be tested
	// CSI hostPath driver test
	Describe("Sanity CSI plugin test using OIM CSI driver", func() {

		var (
			clusterRole                      *rbacv1.ClusterRole
			serviceAccount                   *v1.ServiceAccount
			registryServer, controllerServer *oimcommon.NonBlockingGRPCServer
			controller                       *oimcontroller.Controller
			tmpDir                           string
			ctx                              context.Context
			cancel                           context.CancelFunc
		)

		BeforeEach(func() {
			if spdk.SPDK == nil {
				Skip("No SPDK vhost.")
			}

			ctx, cancel = context.WithCancel(context.Background())
			var err error

			// TODO: test binaries instead or in addition?

			By("starting OIM registry")
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
			registry, err := oimregistry.New()
			Expect(err).NotTo(HaveOccurred())
			hostname, err := os.Hostname()
			Expect(err).NotTo(HaveOccurred())
			registryServer, registryService := oimregistry.Server("tcp4://"+hostname+":0", registry)
			err = registryServer.Start(registryService)
			Expect(err).NotTo(HaveOccurred())
			addr := registryServer.Addr()
			Expect(addr).NotTo(BeNil())
			// No tcp4:/// prefix. It causes gRPC to block?!
			registryAddress := addr.String()

			By("starting OIM controller")
			controllerID := "oim-e2e-controller"
			tmpDir, err = ioutil.TempDir("", "oim-e2e-test")
			Expect(err).NotTo(HaveOccurred())
			controllerAddress := "unix:///" + tmpDir + "/controller.sock"
			controller, err = oimcontroller.New(
				oimcontroller.WithRegistry(registryAddress),
				oimcontroller.WithControllerID(controllerID),
				oimcontroller.WithControllerAddress(controllerAddress),
				oimcontroller.WithVHostController(spdk.VHost),
				oimcontroller.WithVHostDev(spdk.VHostDev),
				oimcontroller.WithSPDK(spdk.SPDKPath),
			)
			controllerServer, controllerService := oimcontroller.Server(controllerAddress, controller)
			err = controllerServer.Start(controllerService)
			Expect(err).NotTo(HaveOccurred())
			err = controller.Start()
			Expect(err).NotTo(HaveOccurred())

			By("deploying CSI OIM driver")
			clusterRole = csiClusterRole(cs, config, false)
			serviceAccount = csiServiceAccount(cs, config, false)
			csiClusterRoleBinding(cs, config, false, serviceAccount, clusterRole)
			csiOIMPod(cs, config, false, f, serviceAccount, registryAddress, controllerID)
			e2eutils.CopyAllLogs(ctx, cs, ns.Name, "csi-pod", GinkgoWriter)
			e2eutils.WatchPods(ctx, cs, GinkgoWriter)
		})

		AfterEach(func() {
			cancel()

			By("uninstalling CSI OIM driver")
			csiOIMPod(cs, config, true, f, serviceAccount, "", "")
			csiClusterRoleBinding(cs, config, true, serviceAccount, clusterRole)
			serviceAccount = csiServiceAccount(cs, config, true)
			clusterRole = csiClusterRole(cs, config, true)

			By("stopping OIM services")
			if registryServer != nil {
				registryServer.ForceStop()
				registryServer.Wait()
			}
			if controllerServer != nil {
				controllerServer.ForceStop()
				controllerServer.Wait()
			}
			if controller != nil {
				controller.Stop()
			}
			if tmpDir != "" {
				os.RemoveAll(tmpDir)
			}
		})

		It("should provision storage", func() {
			t := storageClassTest{
				name:         "oim-csi-driver",
				provisioner:  "oim-csi-driver",
				parameters:   map[string]string{},
				claimSize:    "1Mi",
				expectedSize: "1Mi",
				nodeName:     node.Name,
			}

			claim := newClaim(t, ns.GetName(), "")
			class := newStorageClass(t, ns.GetName(), "")
			claim.Spec.StorageClassName = &class.ObjectMeta.Name
			// TODO: check machine state while volume is mounted:
			// a missing UnmapVolume call in nodeserver.go must be detected
			testDynamicProvisioning(t, cs, claim, class)

			By("verifying that device is gone")
			Eventually(func() (string, error) {
				return qemu.VM.SSH("ls", "-l", "/sys/dev/block")
			}).ShouldNot(ContainSubstring(spdk.VHostDev))
		})
	})
})
