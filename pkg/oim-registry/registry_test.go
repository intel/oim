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

	Describe("storing mapping", func() {
		It("should work", func() {
			db := oimregistry.MemRegistryDB{}
			var err error
			r, err := oimregistry.New(oimregistry.DB(db))
			Expect(err).NotTo(HaveOccurred())
			key1 := "foo/controller-id"
			value1 := "dns:///1.1.1.1/"
			expected := oimregistry.MemRegistryDB{key1: value1}
			_, err = r.SetValue(ctx, &oim.SetValueRequest{
				Value: &oim.Value{
					Path:  key1,
					Value: value1,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(db).To(Equal(expected))

			key2 := "foo/pci"
			value2 := "0000:0003:20.1"
			expected[key2] = value2
			_, err = r.SetValue(ctx, &oim.SetValueRequest{
				Value: &oim.Value{
					Path:  key2,
					Value: value2,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(db).To(Equal(expected))

			key3 := "bar/pci"
			value3 := "0000:0004:30.2"
			expected[key3] = value3
			_, err = r.SetValue(ctx, &oim.SetValueRequest{
				Value: &oim.Value{
					Path:  key3,
					Value: value3,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(db).To(Equal(expected))

			var values *oim.GetValuesReply
			values, err = r.GetValues(ctx, &oim.GetValuesRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key1, Value: value1},
				&oim.Value{Path: key2, Value: value2},
				&oim.Value{Path: key3, Value: value3},
			}))

			values, err = r.GetValues(ctx, &oim.GetValuesRequest{
				Path: "",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key1, Value: value1},
				&oim.Value{Path: key2, Value: value2},
				&oim.Value{Path: key3, Value: value3},
			}))

			values, err = r.GetValues(ctx, &oim.GetValuesRequest{
				Path: key1,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key1, Value: value1},
			}))

			values, err = r.GetValues(ctx, &oim.GetValuesRequest{
				Path: key2,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key2, Value: value2},
			}))

			values, err = r.GetValues(ctx, &oim.GetValuesRequest{
				Path: "foo/",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key1, Value: value1},
				&oim.Value{Path: key2, Value: value2},
			}))

			values, err = r.GetValues(ctx, &oim.GetValuesRequest{
				Path: "/foo///",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key1, Value: value1},
				&oim.Value{Path: key2, Value: value2},
			}))

			values, err = r.GetValues(ctx, &oim.GetValuesRequest{
				Path: "foo",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(values.Values).To(ConsistOf([]*oim.Value{
				&oim.Value{Path: key1, Value: value1},
				&oim.Value{Path: key2, Value: value2},
			}))
		})
	})

	Describe("proxy", func() {
		var (
			tmpDir           string
			registry         *oimregistry.Registry
			registryServer   *oimcommon.NonBlockingGRPCServer
			controllerClient oim.ControllerClient
		)

		BeforeEach(func() {
			var err error

			tmpDir, err = ioutil.TempDir("", "oim-registry-test")
			Expect(err).NotTo(HaveOccurred())

			// Spin up registry.
			registry, err = oimregistry.New()
			Expect(err).NotTo(HaveOccurred())
			registryAddress := "unix://" + filepath.Join(tmpDir, "registry.sock")
			registryServer, service := oimregistry.Server(registryAddress, registry)
			err = registryServer.Start(ctx, service)
			Expect(err).NotTo(HaveOccurred())

			opts := oimcommon.ChooseDialOpts(registryAddress, grpc.WithBlock())
			conn, err := grpc.Dial(registryAddress, opts...)
			Expect(err).NotTo(HaveOccurred())
			controllerClient = oim.NewControllerClient(conn)
		})

		AfterEach(func() {
			if registryServer != nil {
				registryServer.ForceStop(ctx)
				registryServer.Wait(ctx)
			}
			if tmpDir != "" {
				os.RemoveAll(tmpDir)
			}
		})

		It("should fail for unknown controller", func() {
			ctx := metadata.AppendToOutgoingContext(ctx, "controllerid", "no-such-controller")
			_, err := controllerClient.MapVolume(ctx, &oim.MapVolumeRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no-such-controller: no address registered"))
		})

		Context("with controller", func() {
			var (
				controllerID     = "mock-controller"
				controller       *MockController
				controllerServer *oimcommon.NonBlockingGRPCServer
			)

			BeforeEach(func() {
				var err error

				// Spin up controller.
				controller = &MockController{}
				controllerAddress := "unix://" + filepath.Join(tmpDir, "controller.sock")
				controllerServer, service := oimcontroller.Server(controllerAddress, controller)
				err = controllerServer.Start(ctx, service)
				Expect(err).NotTo(HaveOccurred())

				// Register this controller.
				_, err = registry.SetValue(ctx, &oim.SetValueRequest{
					Value: &oim.Value{
						Path:  controllerID + "/" + oimcommon.RegistryAddress,
						Value: controllerAddress,
					},
				})
				Expect(err).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				if controllerServer != nil {
					controllerServer.ForceStop(ctx)
					controllerServer.Wait(ctx)
				}
			})

			It("should work", func() {
				var err error
				var callCtx context.Context

				callCtx = metadata.AppendToOutgoingContext(ctx, "controllerid", "no-such-controller")
				_, err = controllerClient.MapVolume(callCtx, &oim.MapVolumeRequest{})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no-such-controller: no address registered"))

				callCtx = metadata.AppendToOutgoingContext(ctx, "controllerid", controllerID)
				args := oim.MapVolumeRequest{
					VolumeId: "my-volume",
				}
				expected := args
				_, err = controllerClient.MapVolume(callCtx, &args)
				Expect(err).NotTo(HaveOccurred())
				Expect(*controller).To(Equal(MockController{MapVolumes: []oim.MapVolumeRequest{expected}}))
			})
		})
	})
})
