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
	"math/rand"
	"time"

	"k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/intel/oim/test/e2e/framework"
	"github.com/intel/oim/test/e2e/storage/utils"
	clientset "k8s.io/client-go/kubernetes"

	e2eutils "github.com/intel/oim/test/e2e/utils"
	"github.com/intel/oim/test/pkg/spdk"

	. "github.com/onsi/ginkgo"
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
				// TODO: only define this in a Role, as in test/e2e/testing-manifests/storage-csi/controller-role.yaml
				APIGroups: []string{""},
				Resources: []string{"endpoints"},
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
		ctx    = context.Background()
	)

	BeforeEach(func() {
		cs = f.ClientSet
		ns = f.Namespace
		nodes := framework.GetReadySchedulableNodesOrDie(f.ClientSet)
		node = nodes.Items[rand.Intn(len(nodes.Items))]
		config = framework.VolumeTestConfig{
			Namespace:         ns.Name,
			Prefix:            "csi",
			NodeSelector:      map[string]string{"intel.com/oim": "1"},
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

	Describe("Sanity CSI plugin test using OIM CSI with Malloc BDev", func() {

		var (
			clusterRole    *rbacv1.ClusterRole
			serviceAccount *v1.ServiceAccount
			controlPlane   OIMControlPlane
			cleanup        framework.CleanupActionHandle

			afterEach = func() {
				if cleanup == nil {
					return
				}
				framework.RemoveCleanupAction(cleanup)
				cleanup = nil

				By("uninstalling CSI OIM pods")
				csiOIMMalloc(cs, config, true, f, serviceAccount, "", "")
				csiClusterRoleBinding(cs, config, true, serviceAccount, clusterRole)
				serviceAccount = csiServiceAccount(cs, config, true)
				clusterRole = csiClusterRole(cs, config, true)

				controlPlane.StopOIMControlPlane(ctx)
			}
		)

		BeforeEach(func() {
			if spdk.SPDK == nil {
				Skip("No SPDK vhost.")
			}

			controlPlane.StartOIMControlPlane(ctx)

			By("deploying CSI OIM pods")
			clusterRole = csiClusterRole(cs, config, false)
			serviceAccount = csiServiceAccount(cs, config, false)
			csiClusterRoleBinding(cs, config, false, serviceAccount, clusterRole)
			csiOIMMalloc(cs, config, false, f, serviceAccount, controlPlane.registryAddress, controlPlane.controllerID)
			e2eutils.CopyAllLogs(controlPlane.ctx, cs, ns.Name, GinkgoWriter)
			e2eutils.WatchPods(controlPlane.ctx, cs, GinkgoWriter)

			// Always clean up, even when interrupted (https://github.com/onsi/ginkgo/issues/222).
			cleanup = framework.AddCleanupAction(afterEach)
		})

		AfterEach(afterEach)

		It("should provision storage", func() {
			t := storageClassTest{
				name:         "oim-malloc",
				provisioner:  "oim-malloc",
				parameters:   map[string]string{},
				claimSize:    "1Mi",
				expectedSize: "1Mi",
				nodeSelector: map[string]string{"intel.com/oim": "1"},
			}

			claim := newClaim(t, ns.GetName(), "")
			class := newStorageClass(t, ns.GetName(), "")
			claim.Spec.StorageClassName = &class.ObjectMeta.Name
			// TODO: check machine state while volume is mounted:
			// a missing UnmapVolume call in nodeserver.go must be detected
			testDynamicProvisioning(t, cs, claim, class)
		})
	})
})
