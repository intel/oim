/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/pkg/errors"
	"google.golang.org/grpc"

	"github.com/intel/oim/test/e2e/framework"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/kubernetes/pkg/api/legacyscheme"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-controller"
	"github.com/intel/oim/pkg/oim-registry"
	"github.com/intel/oim/pkg/spec/oim/v0"
	"github.com/intel/oim/test/pkg/spdk"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

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
	op.controllerID = "host-0"
	op.tmpDir, err = ioutil.TempDir("", "oim-e2e-test")
	Expect(err).NotTo(HaveOccurred())
	controllerAddress := "unix:///" + op.tmpDir + "/controller.sock"
	op.controller, err = oimcontroller.New(
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

	// Register the controller in the registry.
	opts := oimcommon.ChooseDialOpts(op.registryAddress, grpc.WithInsecure())
	conn, err := grpc.DialContext(ctx, op.registryAddress, opts...)
	Expect(err).NotTo(HaveOccurred())
	defer conn.Close()
	registryClient := oim.NewRegistryClient(conn)
	_, err = registryClient.RegisterController(context.Background(), &oim.RegisterControllerRequest{
		ControllerId: op.controllerID,
		Address:      controllerAddress,
	})
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

// Note:
// - aliases not supported (i.e. use serviceAccountName instead of serviceAccount,
//   for https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.11/#podspec-v1-core)
//   and silently ignored
func CreateFromManifest(f *framework.Framework, mangle func(object interface{}), files ...string) (func(), error) {
	var destructors []func() error

	err := visitManifests(func(data []byte) error {
		// Ignore any additional fields for now, just determine what we have.
		var what What
		if err := runtime.DecodeInto(legacyscheme.Codecs.UniversalDecoder(), data, &what); err != nil {
			return errors.Wrap(err, "decode TypeMeta")
		}

		factory := factories[what]
		if factory == nil {
			return errors.Errorf("item of type %+v not supported", what)
		}

		item := factory.New()
		object := item.ToObject()
		if err := runtime.DecodeInto(legacyscheme.Codecs.UniversalDecoder(), data, object); err != nil {
			return errors.Wrapf(err, "decode %+v", what)
		}

		MangleItem(f, object)
		if mangle != nil {
			mangle(object)
		}

		err, destructor := item.Create(f)
		if err == nil && destructor != nil {
			destructors = append(destructors, destructor)
		}
		return err
	}, files...)

	// Cleaning up can be trigged in two ways:
	// - the test invokes the returned cleanup function,
	//   usually in an AfterEach
	// - the test suite terminates, potentially after
	//   skipping the test's AfterEach (https://github.com/onsi/ginkgo/issues/222)
	var cleanupHandle framework.CleanupActionHandle
	cleanup := func() {
		if cleanupHandle == nil {
			// Already done.
			return
		}
		framework.RemoveCleanupAction(cleanupHandle)

		// TODO (?): use same logic as framework.go for determining
		// whether we clean up?
		for _, destructor := range destructors {
			if err := destructor(); err != nil && !apierrs.IsNotFound(err) {
				framework.Logf("deleting failed: %s", err)
			}
		}
	}
	cleanupHandle = framework.AddCleanupAction(cleanup)
	return cleanup, err
}

type What struct {
	Kind string `json:"kind"`
}

func (in *What) DeepCopy() *What {
	return &What{Kind: in.Kind}
}

func (in *What) DeepCopyInto(out *What) {
	*out = *in
}

func (in *What) DeepCopyObject() runtime.Object {
	return &What{Kind: in.Kind}
}

func (in *What) GetObjectKind() schema.ObjectKind {
	return nil
}

type ItemFactory interface {
	New() Item
}

type Item interface {
	ToObject() runtime.Object
	Create(f *framework.Framework) (error, func() error)
}

var factories = map[What]ItemFactory{
	What{"ServiceAccount"}:     &ServiceAccountFactory{},
	What{"ClusterRole"}:        &ClusterRoleFactory{},
	What{"ClusterRoleBinding"}: &ClusterRoleBindingFactory{},
	What{"StatefulSet"}:        &StatefulSetFactory{},
	What{"DaemonSet"}:          &DaemonSetFactory{},
	What{"StorageClass"}:       &StorageClassFactory{},
}

func visitManifests(cb func(data []byte) error, files ...string) error {
	for _, fileName := range files {
		By(fmt.Sprintf("parsing %s", fileName))
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

		// Split at the "---" separator before working on
		// individual item. We need to split ourselves because
		// we need access to each original chunk of data for
		// runtime.DecodeInto. kubectl has its own
		// infrastructure for this, but that is a lot of code
		// with many dependencies.
		items := bytes.Split(data, []byte("\n---"))

		for _, item := range items {
			if err := cb(item); err != nil {
				return errors.Wrap(err, fileName)
			}
		}
	}
	return nil
}

// MangleName makes the name of some entity unique by appending the
// generated unique name.
func MangleName(f *framework.Framework, item *string) {
	if *item != "" {
		*item = *item + "-" + f.UniqueName
	}
}

// MangleNamespace moves the entity into the test's namespace.  Not
// all entities can be namespaced. For those, the name also needs to be
// mangled.
func MangleNamespace(f *framework.Framework, item *string) {
	if f.Namespace != nil {
		*item = f.Namespace.GetName()
	}
}

// MangleItem recursively fixes the name and namespace of an entity
// such that each test gets its own private copy of the entity which
// in turn uses the private copies of the other entities created by
// the test.
func MangleItem(f *framework.Framework, item interface{}) {
	By(fmt.Sprintf("mangling original content of %T:\n%s", item, PrettyPrint(item)))
	mangleItemRecursively(f, item)
}

func mangleItemRecursively(f *framework.Framework, item interface{}) {
	switch item := item.(type) {
	case *v1.ObjectMeta:
		MangleName(f, &item.Name)
		MangleNamespace(f, &item.Namespace)
	case *metav1.ObjectMeta:
		MangleName(f, &item.Name)
		MangleNamespace(f, &item.Namespace)
	case *rbacv1.Subject:
		MangleName(f, &item.Name)
		MangleNamespace(f, &item.Namespace)
	case *rbacv1.RoleRef:
		MangleName(f, &item.Name)
	case *rbacv1.ClusterRole:
		MangleItem(f, &item.ObjectMeta)
	case *v1.ServiceAccount:
		MangleItem(f, &item.ObjectMeta)
	case *rbacv1.ClusterRoleBinding:
		MangleItem(f, &item.ObjectMeta)
		for i := range item.Subjects {
			MangleItem(f, &item.Subjects[i])
		}
		MangleItem(f, &item.RoleRef)
	case *appsv1.StatefulSet:
		MangleNamespace(f, &item.ObjectMeta.Namespace)
		MangleName(f, &item.Spec.Template.Spec.ServiceAccountName)
		// TODO: mangle CSI driver name? How?
	case *appsv1.DaemonSet:
		MangleNamespace(f, &item.ObjectMeta.Namespace)
		MangleName(f, &item.Spec.Template.Spec.ServiceAccountName)
	case *storagev1.StorageClass:
		// TODO: if we mangle the CSI driver name, then we need to mangle the Provisioner here, too.
		MangleItem(f, &item.ObjectMeta)
	default:
		framework.Failf("missing support for mangling item of type %T", item)
	}
}

// The individual factories all follow the same template, but with
// enough differences in types and functions that copy-and-paste
// looked like the least dirty approach. Perhaps one day Go will have
// generics.

type ServiceAccountFactory struct{}

func (f *ServiceAccountFactory) New() Item {
	return &ServiceAccountItem{}
}

type ServiceAccountItem v1.ServiceAccount

func (item *ServiceAccountItem) ToObject() runtime.Object {
	return (*v1.ServiceAccount)(item)
}

func (item *ServiceAccountItem) Create(f *framework.Framework) (error, func() error) {
	client := f.ClientSet.CoreV1().ServiceAccounts(f.Namespace.GetName())
	By(fmt.Sprintf("creating ServiceAccount:\n%s", PrettyPrint(item)))
	_, err := client.Create((*v1.ServiceAccount)(item))
	return errors.Wrap(err, "create ServiceAccount"), func() error {
		framework.Logf("deleting ServiceAccount %s", item.GetName())
		return client.Delete(item.GetName(), &metav1.DeleteOptions{})
	}
}

type ClusterRoleFactory struct{}

func (f *ClusterRoleFactory) New() Item {
	return &ClusterRoleItem{}
}

type ClusterRoleItem rbacv1.ClusterRole

func (item *ClusterRoleItem) ToObject() runtime.Object {
	return (*rbacv1.ClusterRole)(item)
}

func (item *ClusterRoleItem) Create(f *framework.Framework) (error, func() error) {
	// Impersonation is required for Kubernetes < 1.12, see
	// https://github.com/kubernetes/kubernetes/issues/62237#issuecomment-429315111
	By("Creating an impersonating superuser kubernetes clientset to define cluster role")
	rc, err := framework.LoadConfig()
	framework.ExpectNoError(err)
	rc.Impersonate = restclient.ImpersonationConfig{
		UserName: "superuser",
		Groups:   []string{"system:masters"},
	}
	superuserClientset, err := clientset.NewForConfig(rc)
	framework.ExpectNoError(err, "create superuser clientset")

	client := superuserClientset.RbacV1().ClusterRoles()
	By(fmt.Sprintf("creating ClusterRole\n:%s", PrettyPrint(item)))
	_, err = client.Create((*rbacv1.ClusterRole)(item))
	return errors.Wrap(err, "create ClusterRole"), func() error {
		framework.Logf("deleting ClusterRole %s", item.GetName())
		return client.Delete(item.GetName(), &metav1.DeleteOptions{})
	}
}

type ClusterRoleBindingFactory struct{}

func (f *ClusterRoleBindingFactory) New() Item {
	return &ClusterRoleBindingItem{}
}

type ClusterRoleBindingItem rbacv1.ClusterRoleBinding

func (item *ClusterRoleBindingItem) ToObject() runtime.Object {
	return (*rbacv1.ClusterRoleBinding)(item)
}

func (item *ClusterRoleBindingItem) Create(f *framework.Framework) (error, func() error) {
	client := f.ClientSet.RbacV1().ClusterRoleBindings()
	By(fmt.Sprintf("creating ClusterRoleBinding:\n%s", PrettyPrint(item)))
	_, err := client.Create((*rbacv1.ClusterRoleBinding)(item))
	return errors.Wrap(err, "create ClusterRoleBinding"), func() error {
		framework.Logf("deleting ClusterRoleBinding %s", item.GetName())
		return client.Delete(item.GetName(), &metav1.DeleteOptions{})
	}
}

type StatefulSetFactory struct{}

func (f *StatefulSetFactory) New() Item {
	return &StatefulSetItem{}
}

type StatefulSetItem appsv1.StatefulSet

func (item *StatefulSetItem) ToObject() runtime.Object {
	return (*appsv1.StatefulSet)(item)
}

func (item *StatefulSetItem) Create(f *framework.Framework) (error, func() error) {
	client := f.ClientSet.AppsV1().StatefulSets(f.Namespace.GetName())
	By(fmt.Sprintf("creating StatefulSet:\n%s", PrettyPrint(item)))
	_, err := client.Create((*appsv1.StatefulSet)(item))
	return errors.Wrap(err, "create StatefulSet"), func() error {
		framework.Logf("deleting StatefulSet %s", item.GetName())
		return client.Delete(item.GetName(), &metav1.DeleteOptions{})
	}
}

type DaemonSetFactory struct{}

func (f *DaemonSetFactory) New() Item {
	return &DaemonSetItem{}
}

type DaemonSetItem appsv1.DaemonSet

func (item *DaemonSetItem) ToObject() runtime.Object {
	return (*appsv1.DaemonSet)(item)
}

func (item *DaemonSetItem) Create(f *framework.Framework) (error, func() error) {
	client := f.ClientSet.AppsV1().DaemonSets(f.Namespace.GetName())
	By(fmt.Sprintf("creating DaemonSet:\n%s", PrettyPrint(item)))
	_, err := client.Create((*appsv1.DaemonSet)(item))
	return errors.Wrap(err, "create DaemonSet"), func() error {
		framework.Logf("deleting DaemonSet %s", item.GetName())
		return client.Delete(item.GetName(), &metav1.DeleteOptions{})
	}
}

type StorageClassFactory struct{}

func (f *StorageClassFactory) New() Item {
	return &StorageClassItem{}
}

type StorageClassItem storagev1.StorageClass

func (item *StorageClassItem) ToObject() runtime.Object {
	return (*storagev1.StorageClass)(item)
}

func (item *StorageClassItem) Create(f *framework.Framework) (error, func() error) {
	client := f.ClientSet.StorageV1().StorageClasses()
	By(fmt.Sprintf("creating StorageClass:\n%s", PrettyPrint(item)))
	_, err := client.Create((*storagev1.StorageClass)(item))
	return errors.Wrap(err, "create StorageClass"), func() error {
		framework.Logf("deleting StorageClass %s", item.GetName())
		return client.Delete(item.GetName(), &metav1.DeleteOptions{})
	}
}

func PrettyPrint(item interface{}) string {
	data, err := json.MarshalIndent(item, "", "  ")
	if err == nil {
		return string(data)
	}
	return fmt.Sprintf("%+v", item)
}
