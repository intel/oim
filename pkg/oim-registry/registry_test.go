/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimregistry_test

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-controller"
	"github.com/intel/oim/pkg/oim-registry"
	"github.com/intel/oim/pkg/spec/oim/v0"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// MockController implements oim.Controller.
type MockController struct {
	MapVolumes           []oim.MapVolumeRequest
	UnmapVolumes         []oim.UnmapVolumeRequest
	ProvisionMallocBDevs []oim.ProvisionMallocBDevRequest
	CheckMallocBDevs     []oim.CheckMallocBDevRequest
}

func (m *MockController) MapVolume(ctx context.Context, in *oim.MapVolumeRequest) (*oim.MapVolumeReply, error) {
	m.MapVolumes = append(m.MapVolumes, *in)
	return &oim.MapVolumeReply{}, nil
}

func (m *MockController) UnmapVolume(ctx context.Context, in *oim.UnmapVolumeRequest) (*oim.UnmapVolumeReply, error) {
	m.UnmapVolumes = append(m.UnmapVolumes, *in)
	return &oim.UnmapVolumeReply{}, nil
}

func (m *MockController) ProvisionMallocBDev(ctx context.Context, in *oim.ProvisionMallocBDevRequest) (*oim.ProvisionMallocBDevReply, error) {
	m.ProvisionMallocBDevs = append(m.ProvisionMallocBDevs, *in)
	return &oim.ProvisionMallocBDevReply{}, nil
}

func (m *MockController) CheckMallocBDev(ctx context.Context, in *oim.CheckMallocBDevRequest) (*oim.CheckMallocBDevReply, error) {
	m.CheckMallocBDevs = append(m.CheckMallocBDevs, *in)
	return &oim.CheckMallocBDevReply{}, nil
}

var _ = Describe("OIM Registry", func() {
	ctx := context.Background()
	adminCtx := oimregistry.RegistryClientContext(ctx, "user.admin")

	Describe("storing mapping", func() {
		It("should work", func() {
			db := oimregistry.NewMemRegistryDB()
			var err error
			tlsConfig, err := oimcommon.LoadTLSConfig(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"), os.ExpandEnv("${TEST_WORK}/ca/component.registry.key"), "")
			Expect(err).NotTo(HaveOccurred())
			r, err := oimregistry.New(oimregistry.DB(db), oimregistry.TLS(tlsConfig))
			Expect(err).NotTo(HaveOccurred())
			key1 := "foo/controller-id"
			value1 := "dns:///1.1.1.1/"
			expected := map[string]string{key1: value1}
			_, err = r.SetValue(adminCtx, &oim.SetValueRequest{
				Value: &oim.Value{
					Path:  key1,
					Value: value1,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(oimregistry.GetRegistryEntries(db)).To(Equal(expected))

			key2 := "foo/pci"
			value2 := "0000:0003:20.1"
			expected[key2] = value2
			_, err = r.SetValue(adminCtx, &oim.SetValueRequest{
				Value: &oim.Value{
					Path:  key2,
					Value: value2,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(oimregistry.GetRegistryEntries(db)).To(Equal(expected))

			key3 := "bar/pci"
			value3 := "0000:0004:30.2"
			expected[key3] = value3
			_, err = r.SetValue(adminCtx, &oim.SetValueRequest{
				Value: &oim.Value{
					Path:  key3,
					Value: value3,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(oimregistry.GetRegistryEntries(db)).To(Equal(expected))

			var values *oim.GetValuesReply
			values, err = r.GetValues(adminCtx, &oim.GetValuesRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key1, Value: value1},
				&oim.Value{Path: key2, Value: value2},
				&oim.Value{Path: key3, Value: value3},
			}))

			values, err = r.GetValues(adminCtx, &oim.GetValuesRequest{
				Path: "",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key1, Value: value1},
				&oim.Value{Path: key2, Value: value2},
				&oim.Value{Path: key3, Value: value3},
			}))

			values, err = r.GetValues(adminCtx, &oim.GetValuesRequest{
				Path: key1,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key1, Value: value1},
			}))

			values, err = r.GetValues(adminCtx, &oim.GetValuesRequest{
				Path: key2,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key2, Value: value2},
			}))

			values, err = r.GetValues(adminCtx, &oim.GetValuesRequest{
				Path: "foo/",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key1, Value: value1},
				&oim.Value{Path: key2, Value: value2},
			}))

			values, err = r.GetValues(adminCtx, &oim.GetValuesRequest{
				Path: "/foo///",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key1, Value: value1},
				&oim.Value{Path: key2, Value: value2},
			}))

			values, err = r.GetValues(adminCtx, &oim.GetValuesRequest{
				Path: "foo",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key1, Value: value1},
				&oim.Value{Path: key2, Value: value2},
			}))
		})
	})

	Describe("server", func() {
		var (
			controllerID     = "host-0"
			tmpDir           string
			registry         *oimregistry.Registry
			registryServer   *oimcommon.NonBlockingGRPCServer
			registryAddress  string
			controllerClient oim.ControllerClient
			clientConn       *grpc.ClientConn
		)

		BeforeEach(func() {
			var err error

			tmpDir, err = ioutil.TempDir("", "oim-registry-test")
			Expect(err).NotTo(HaveOccurred())

			// Spin up registry.
			tlsConfig, err := oimcommon.LoadTLSConfig(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"), os.ExpandEnv("${TEST_WORK}/ca/component.registry.key"), "")
			Expect(err).NotTo(HaveOccurred())
			registry, err = oimregistry.New(oimregistry.TLS(tlsConfig))
			Expect(err).NotTo(HaveOccurred())
			registryAddress = "unix://" + filepath.Join(tmpDir, "registry.sock")
			registryServer, service := registry.Server(registryAddress)
			err = registryServer.Start(ctx, service)
			Expect(err).NotTo(HaveOccurred())

			// Set up a controller client which goes through the registry's proxy.
			clientCreds, err := oimcommon.LoadTLS(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"),
				os.ExpandEnv("${TEST_WORK}/ca/host."+controllerID),
				"component.registry")
			Expect(err).NotTo(HaveOccurred())
			opts := oimcommon.ChooseDialOpts(registryAddress, grpc.WithBlock(), grpc.WithTransportCredentials(clientCreds))
			clientConn, err = grpc.Dial(registryAddress, opts...)
			Expect(err).NotTo(HaveOccurred())
			controllerClient = oim.NewControllerClient(clientConn)
		})

		AfterEach(func() {
			if registryServer != nil {
				registryServer.ForceStop(ctx)
				registryServer.Wait(ctx)
			}
			if tmpDir != "" {
				os.RemoveAll(tmpDir)
			}
			if clientConn != nil {
				clientConn.Close()
			}
		})

		It("should fail for missing metadata", func() {
			_, err := controllerClient.MapVolume(ctx, &oim.MapVolumeRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(`code = FailedPrecondition desc = missing or invalid controllerid meta data`))
		})

		It("should fail for unknown controller", func() {
			ctx := metadata.AppendToOutgoingContext(ctx, "controllerid", "host-0")
			_, err := controllerClient.MapVolume(ctx, &oim.MapVolumeRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("host-0: no address registered"))
		})

		It("should fail for wrong controller", func() {
			ctx := metadata.AppendToOutgoingContext(ctx, "controllerid", "host-1")
			_, err := controllerClient.MapVolume(ctx, &oim.MapVolumeRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(`code = PermissionDenied desc = caller "host.host-0" not allowed to contact controller "host-1"`))
		})

		It("should reject normal user SetVar", func() {
			registryClient := oim.NewRegistryClient(clientConn)
			_, err := registryClient.SetValue(ctx, &oim.SetValueRequest{
				Value: &oim.Value{
					Path:  "foo",
					Value: "bar",
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(`code = PermissionDenied desc = caller "host.host-0" not allowed to set "foo"`))
		})

		Context("with client", func() {
			var (
				ca       = os.ExpandEnv("${TEST_WORK}/ca/ca.crt")
				key      = os.ExpandEnv("${TEST_WORK}/ca/host." + controllerID + ".key")
				evilCA   = os.ExpandEnv("${TEST_WORK}/evil-ca/evil-ca.crt")
				evilKey  = os.ExpandEnv("${TEST_WORK}/evil-ca/host." + controllerID + ".key")
				wrongKey = os.ExpandEnv("${TEST_WORK}/ca/host.host-1.key")
			)

			// This covers different scenarios for connections to the OIM registry.
			cases := []struct {
				name, ca, key, peer, errorText string
			}{
				{"should be accepted", ca, key, "component.registry", "host-0: no address registered"},
				{"registry should detect man-in-the-middle", ca, evilKey, "component.registry", "authentication handshake failed: remote error: tls: bad certificate"},
				{"client should detect man-in-the-middle", evilCA, evilKey, "component.registry", "transport: authentication handshake failed: x509: certificate signed by unknown authority"},
				{"registry should detect wrong host", ca, wrongKey, "component.registry", `caller "host.host-1" not allowed to contact controller "host-0"`},
				{"client should detect wrong peer", ca, key, "component.foobar", "transport: authentication handshake failed: x509: certificate is valid for component.registry, not component.foobar"},
			}

			for _, c := range cases {
				c := c
				It(c.name, func() {
					// Set up a controller client which goes through the registry's proxy.
					clientCreds, err := oimcommon.LoadTLS(c.ca, c.key, c.peer)
					Expect(err).NotTo(HaveOccurred())
					opts := oimcommon.ChooseDialOpts(registryAddress, grpc.WithTransportCredentials(clientCreds))
					conn, err := grpc.Dial(registryAddress, opts...)
					Expect(err).NotTo(HaveOccurred())
					controllerClient = oim.NewControllerClient(conn)

					callCtx := metadata.AppendToOutgoingContext(ctx, "controllerid", controllerID)
					args := oim.MapVolumeRequest{
						VolumeId: "my-volume",
					}
					_, err = controllerClient.MapVolume(callCtx, &args)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(c.errorText))
				})
			}
		})

		Context("with controller", func() {
			var (
				controller       *MockController
				controllerServer *oimcommon.NonBlockingGRPCServer

				ca       = os.ExpandEnv("${TEST_WORK}/ca/ca.crt")
				key      = os.ExpandEnv("${TEST_WORK}/ca/controller." + controllerID + ".key")
				evilCA   = os.ExpandEnv("${TEST_WORK}/evil-ca/evil-ca.crt")
				evilKey  = os.ExpandEnv("${TEST_WORK}/evil-ca/controller." + controllerID + ".key")
				wrongKey = os.ExpandEnv("${TEST_WORK}/ca/controller.host-1.key")
			)

			setupController := func(ca, key string) {
				var err error

				// Spin up controller.
				controllerCreds, err := oimcommon.LoadTLS(ca, key, "component.registry")
				Expect(err).NotTo(HaveOccurred())
				controller = &MockController{}
				controllerAddress := "unix://" + filepath.Join(tmpDir, "controller.sock")
				controllerServer, service := oimcontroller.Server(controllerAddress, controller, controllerCreds)
				err = controllerServer.Start(ctx, service)
				Expect(err).NotTo(HaveOccurred())

				_, err = registry.SetValue(adminCtx, &oim.SetValueRequest{
					Value: &oim.Value{
						Path:  controllerID + "/" + oimcommon.RegistryAddress,
						Value: controllerAddress,
					},
				})
			}

			AfterEach(func() {
				if controllerServer != nil {
					controllerServer.ForceStop(ctx)
					controllerServer.Wait(ctx)
				}
			})

			// This covers different scenarios for connections to the OIM controller.
			cases := []struct {
				name, ca, key, errorText string
			}{
				{"should work", ca, key, ""},
				{"controller should detect man-in-the-middle", evilCA, key, "authentication handshake failed: remote error: tls: bad certificate"},
				{"registry should detect man-in-the-middle", ca, evilKey, "transport: authentication handshake failed: x509: certificate signed by unknown authority"},
				{"registry should detect wrong controller", ca, wrongKey, "transport: authentication handshake failed: x509: certificate is valid for controller.host-1, not controller.host-0"},
			}

			for _, c := range cases {
				c := c
				It(c.name, func() {
					var err error

					// Set up and register this controller.
					setupController(c.ca, c.key)
					Expect(err).NotTo(HaveOccurred())

					callCtx := metadata.AppendToOutgoingContext(ctx, "controllerid", controllerID)
					args := oim.MapVolumeRequest{
						VolumeId: "my-volume",
					}
					expected := args
					_, err = controllerClient.MapVolume(callCtx, &args)
					if c.errorText == "" {
						Expect(err).NotTo(HaveOccurred())
						Expect(*controller).To(Equal(MockController{MapVolumes: []oim.MapVolumeRequest{expected}}))
					} else {
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring(c.errorText))
					}
				})
			}
		})
	})
})
