/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package storage

import (
	"context"
	"io/ioutil"
	"os"

	"google.golang.org/grpc"

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
	tlsConfig, err := oimcommon.LoadTLSConfig(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"), os.ExpandEnv("${TEST_WORK}/ca/component.registry.key"), "")
	Expect(err).NotTo(HaveOccurred())
	registry, err := oimregistry.New(oimregistry.TLS(tlsConfig))
	Expect(err).NotTo(HaveOccurred())
	hostname, err := os.Hostname()
	Expect(err).NotTo(HaveOccurred())
	rs, registryService := registry.Server("tcp4://" + hostname + ":0")
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
	transportCreds, err := oimcommon.LoadTLS(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"), os.ExpandEnv("${TEST_WORK}/ca/controller.host-0.key"), "component.registry")
	Expect(err).NotTo(HaveOccurred())
	controllerAddress := "unix:///" + op.tmpDir + "/controller.sock"
	op.controller, err = oimcontroller.New(
		oimcontroller.WithVHostController(spdk.VHost),
		oimcontroller.WithVHostDev(":.0"), // Only PCI function provided by controller, rest comes from registry.
		oimcontroller.WithSPDK(spdk.SPDKPath),
		oimcontroller.WithCreds(transportCreds),
	)
	Expect(err).NotTo(HaveOccurred())
	controllerCreds, err := oimcommon.LoadTLS(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"), os.ExpandEnv("${TEST_WORK}/ca/controller.host-0.key"), "component.registry")
	Expect(err).NotTo(HaveOccurred())
	cs, controllerService := oimcontroller.Server(controllerAddress, op.controller, controllerCreds)
	op.controllerServer = cs
	err = op.controllerServer.Start(ctx, controllerService)
	Expect(err).NotTo(HaveOccurred())
	err = op.controller.Start()
	Expect(err).NotTo(HaveOccurred())

	// Register the controller in the registry.
	clientCreds, err := oimcommon.LoadTLS(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"), os.ExpandEnv("${TEST_WORK}/ca/user.admin.key"), "component.registry")
	Expect(err).NotTo(HaveOccurred())
	opts := oimcommon.ChooseDialOpts(op.registryAddress, grpc.WithTransportCredentials(clientCreds))
	conn, err := grpc.DialContext(ctx, op.registryAddress, opts...)
	Expect(err).NotTo(HaveOccurred())
	defer conn.Close()
	registryClient := oim.NewRegistryClient(conn)
	_, err = registryClient.SetValue(context.Background(), &oim.SetValueRequest{
		Value: &oim.Value{
			Path:  op.controllerID + "/" + oimcommon.RegistryAddress,
			Value: controllerAddress,
		},
	})
	Expect(err).NotTo(HaveOccurred())
	_, err = registryClient.SetValue(context.Background(), &oim.SetValueRequest{
		Value: &oim.Value{
			Path:  op.controllerID + "/" + oimcommon.RegistryPCI,
			Value: spdk.VHostDev,
		},
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
