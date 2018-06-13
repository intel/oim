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

	"github.com/mwitkow/grpc-proxy/proxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-registry"
	"github.com/intel/oim/pkg/spec/oim/v0"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// MockController implements oim.Controller.
type MockController struct {
	MapVolumes   []oim.MapVolumeRequest
	UnmapVolumes []oim.UnmapVolumeRequest
}

func (m *MockController) MapVolume(ctx context.Context, in *oim.MapVolumeRequest) (*oim.MapVolumeReply, error) {
	m.MapVolumes = append(m.MapVolumes, *in)
	return &oim.MapVolumeReply{}, nil
}

func (m *MockController) UnmapVolume(ctx context.Context, in *oim.UnmapVolumeRequest) (*oim.UnmapVolumeReply, error) {
	return &oim.UnmapVolumeReply{}, nil
}

var _ = Describe("OIM Registry", func() {
	ctx := context.Background()

	Describe("storing mapping", func() {
		It("should work", func() {
			db := oimregistry.MemRegistryDB{}
			var err error
			r, err := oimregistry.New(oimregistry.DB(db))
			Expect(err).NotTo(HaveOccurred())
			hardwareID := "foo"
			address := "tpc:///1.1.1.1/"
			_, err = r.RegisterController(ctx, &oim.RegisterControllerRequest{
				UUID:    hardwareID,
				Address: address,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(db).To(Equal(oimregistry.MemRegistryDB{hardwareID: address}))
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
			registryServer = &oimcommon.NonBlockingGRPCServer{
				Endpoint: registryAddress,
				ServerOptions: []grpc.ServerOption{
					grpc.CustomCodec(proxy.Codec()),
					grpc.UnknownServiceHandler(proxy.TransparentHandler(registry.StreamDirector())),
				},
			}
			service := func(s *grpc.Server) {
				oim.RegisterRegistryServer(s, registry)
			}
			err = registryServer.Start(service)
			Expect(err).NotTo(HaveOccurred())

			conn, err := grpc.Dial(registryAddress, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithDialer(oimcommon.ChooseDialer(registryAddress)))
			Expect(err).NotTo(HaveOccurred())
			controllerClient = oim.NewControllerClient(conn)
		})

		AfterEach(func() {
			if registryServer != nil {
				registryServer.ForceStop()
				registryServer.Wait()
			}
			if tmpDir != "" {
				os.RemoveAll(tmpDir)
			}
		})

		It("should fail for unknown hardware", func() {
			ctx := metadata.AppendToOutgoingContext(ctx, "hardwareid", "no-such-hardware")
			_, err := controllerClient.MapVolume(ctx, &oim.MapVolumeRequest{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no-such-hardware: not registered"))
		})

		Context("with hardware", func() {
			var (
				hardwareID       = "mock-hardware"
				controller       *MockController
				controllerServer *oimcommon.NonBlockingGRPCServer
			)

			BeforeEach(func() {
				var err error

				// Spin up controller.
				controller = &MockController{}
				controllerAddress := "unix://" + filepath.Join(tmpDir, "controller.sock")
				controllerServer = &oimcommon.NonBlockingGRPCServer{
					Endpoint: controllerAddress,
					ServerOptions: []grpc.ServerOption{
						grpc.CustomCodec(proxy.Codec()),
					},
				}
				service := func(s *grpc.Server) {
					oim.RegisterControllerServer(s, controller)
				}
				err = controllerServer.Start(service)
				Expect(err).NotTo(HaveOccurred())

				// Register this controller.
				_, err = registry.RegisterController(ctx, &oim.RegisterControllerRequest{
					UUID:    hardwareID,
					Address: controllerAddress,
				})
				Expect(err).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				if controllerServer != nil {
					controllerServer.ForceStop()
					controllerServer.Wait()
				}
			})

			It("should work", func() {
				var err error
				var callCtx context.Context

				callCtx = metadata.AppendToOutgoingContext(ctx, "hardwareid", "no-such-hardware")
				_, err = controllerClient.MapVolume(callCtx, &oim.MapVolumeRequest{})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no-such-hardware: not registered"))

				callCtx = metadata.AppendToOutgoingContext(ctx, "hardwareid", hardwareID)
				args := oim.MapVolumeRequest{
					UUID: "my-volume",
				}
				expected := args
				_, err = controllerClient.MapVolume(callCtx, &args)
				Expect(err).NotTo(HaveOccurred())
				Expect(*controller).To(Equal(MockController{MapVolumes: []oim.MapVolumeRequest{expected}}))
			})
		})
	})
})
