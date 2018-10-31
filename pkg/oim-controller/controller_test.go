/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcontroller_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/log/level"
	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-controller"
	"github.com/intel/oim/pkg/oim-registry"
	"github.com/intel/oim/pkg/spdk"
	"github.com/intel/oim/pkg/spec/oim/v0"
	"github.com/intel/oim/test/pkg/qemu"
	testspdk "github.com/intel/oim/test/pkg/spdk"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("OIM Controller", func() {
	var (
		c               *oimcontroller.Controller
		ctx             context.Context
		controllerCreds credentials.TransportCredentials
	)

	BeforeEach(func() {
		var err error
		controllerCreds, err = oimcommon.LoadTLS(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"), os.ExpandEnv("${TEST_WORK}/ca/controller.host-0.key"), "component.registry")
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("registration", func() {
		var (
			db              *oimregistry.MemRegistryDB
			registry        *oimregistry.Registry
			registryServer  *oimcommon.NonBlockingGRPCServer
			registryAddress string
		)

		BeforeEach(func() {
			var err error

			ctx = log.WithLogger(context.Background(),
				log.NewSimpleLogger(log.SimpleConfig{
					Level:  level.Debug,
					Output: GinkgoWriter,
				}).With("at", "oim-registry"))

			// Spin up registry.
			tlsConfig, err := oimcommon.LoadTLSConfig(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"), os.ExpandEnv("${TEST_WORK}/ca/component.registry.key"), "")
			Expect(err).NotTo(HaveOccurred())
			db = &oimregistry.MemRegistryDB{}
			registry, err = oimregistry.New(oimregistry.DB(db), oimregistry.TLS(tlsConfig))
			Expect(err).NotTo(HaveOccurred())
			registryServer, service := registry.Server("tcp4://:0")
			err = registryServer.Start(ctx, service)
			Expect(err).NotTo(HaveOccurred())
			addr := registryServer.Addr()
			Expect(addr).NotTo(BeNil())
			// No tcp4:/// prefix. It causes gRPC to block?!
			registryAddress = addr.String()
		})

		AfterEach(func() {
			if registryServer != nil {
				registryServer.ForceStop(ctx)
				registryServer.Wait(ctx)
			}
		})

		It("should work", func() {
			addr := "foo://bar"
			controllerID := "host-0"
			c, err := oimcontroller.New(
				oimcontroller.WithRegistry(registryAddress),
				oimcontroller.WithCreds(controllerCreds),
				oimcontroller.WithControllerID(controllerID),
				oimcontroller.WithControllerAddress(addr),
			)
			err = c.Start()
			Expect(err).NotTo(HaveOccurred())
			defer c.Stop()

			Eventually(func() oimregistry.MemRegistryDB {
				return *db
			}).Should(Equal(oimregistry.MemRegistryDB{controllerID + "/" + oimcommon.RegistryAddress: addr}))
		})

		It("should re-register", func() {
			addr := "foo://bar"
			controllerID := "host-0"
			c, err := oimcontroller.New(
				oimcontroller.WithRegistry(registryAddress),
				oimcontroller.WithCreds(controllerCreds),
				oimcontroller.WithControllerID(controllerID),
				oimcontroller.WithControllerAddress(addr),
				oimcontroller.WithRegistryDelay(5*time.Second),
			)
			err = c.Start()
			Expect(err).NotTo(HaveOccurred())
			defer c.Stop()

			getDB := func() oimregistry.MemRegistryDB {
				return *db
			}
			Eventually(getDB, 1*time.Second).Should(Equal(oimregistry.MemRegistryDB{controllerID + "/" + oimcommon.RegistryAddress: addr}))
			(*db)[controllerID+"/"+oimcommon.RegistryAddress] = ""
			Consistently(getDB, 4*time.Second).Should(Equal(oimregistry.MemRegistryDB{controllerID + "/" + oimcommon.RegistryAddress: ""}))
			Eventually(getDB, 120*time.Second).Should(Equal(oimregistry.MemRegistryDB{controllerID + "/" + oimcommon.RegistryAddress: addr}))
		})

		It("should really stop", func() {
			addr := "foo://bar"
			controllerID := "host-0"
			c, err := oimcontroller.New(
				oimcontroller.WithRegistry(registryAddress),
				oimcontroller.WithCreds(controllerCreds),
				oimcontroller.WithControllerID(controllerID),
				oimcontroller.WithControllerAddress(addr),
				oimcontroller.WithRegistryDelay(5*time.Second),
			)
			err = c.Start()
			Expect(err).NotTo(HaveOccurred())

			getDB := func() oimregistry.MemRegistryDB {
				return *db
			}
			Eventually(getDB, 1*time.Second).Should(Equal(oimregistry.MemRegistryDB{controllerID + "/" + oimcommon.RegistryAddress: addr}))
			c.Stop()
			(*db)[controllerID+"/"+oimcommon.RegistryAddress] = ""
			Consistently(getDB, 10*time.Second).Should(Equal(oimregistry.MemRegistryDB{controllerID + "/" + oimcommon.RegistryAddress: ""}))
		})
	})

	Describe("attaching a volume", func() {
		var (
			// Names must match for MapVolume to succeed.
			volumeID = "controller-test"
			bdevName = volumeID
			bdevArgs = oim.ProvisionMallocBDevRequest{
				BdevName: bdevName,
				Size_:    1 * 1024 * 1024,
			}
		)

		BeforeEach(func() {
			var err error

			err = testspdk.Init(testspdk.WithVHostSCSI())
			Expect(err).NotTo(HaveOccurred())
			if testspdk.SPDK == nil {
				Skip("No SPDK vhost.")
			}

			// TODO: add logging wrapper around service.
			// Otherwise function calls into this
			// controller are not getting logged because
			// we are not using gRPC.
			c, err = oimcontroller.New(oimcontroller.WithSPDK(testspdk.SPDKPath),
				oimcontroller.WithCreds(controllerCreds),
				oimcontroller.WithVHostDev(testspdk.VHostDev),
				oimcontroller.WithVHostController(testspdk.VHostPath))
			Expect(err).NotTo(HaveOccurred())

			_, err = c.ProvisionMallocBDev(context.Background(), &bdevArgs)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			failed := []error{}

			// Clean up all bdevs and thus also VHost LUNs which might
			// have been created during testing.
			if testspdk.SPDK != nil {

				bdevArgs := oim.ProvisionMallocBDevRequest{
					BdevName: bdevName,
				}
				_, err := c.ProvisionMallocBDev(context.Background(), &bdevArgs)
				if err != nil {
					failed = append(failed, fmt.Errorf("ProvisionMallocBDev: %s", err))
				}

				bdevs, err := spdk.GetBDevs(context.Background(), c.SPDK, spdk.GetBDevsArgs{})
				if err != nil {
					failed = append(failed, errors.New(fmt.Sprintf("GetBDevs: %s", err)))
					bdevs = spdk.GetBDevsResponse{}
				}

				for _, bdev := range bdevs {
					args := spdk.DeleteBDevArgs{
						Name: bdev.Name,
					}
					if err := spdk.DeleteBDev(context.Background(), c.SPDK, args); err != nil {
						failed = append(failed, errors.New(fmt.Sprintf("DeleteBDev %s: %s", bdev.Name, err)))
					}
				}
			}

			// And also the VHost controller.
			if err := testspdk.Finalize(); err != nil {
				failed = append(failed, fmt.Errorf("spdk.Finalize: %s", err))
			}

			Expect(failed).To(BeEmpty())
		})

		It("should fail when missing parameters", func() {
			request := oim.MapVolumeRequest{
				VolumeId: "foobar",
			}
			_, err := c.MapVolume(context.Background(), &request)
			Expect(err).To(HaveOccurred())
		})

		mapVolume := func() (oim.MapVolumeRequest, spdk.GetVHostControllersResponse) {
			var err error
			ctx := context.Background()

			add := oim.MapVolumeRequest{
				VolumeId: volumeID,
				Params: &oim.MapVolumeRequest_Malloc{
					Malloc: &oim.MallocParams{},
				},
			}
			log.L().Info("before MapVolume")
			reply, err := c.MapVolume(context.Background(), &add)
			log.L().Info("after MapVolume")
			Expect(err).NotTo(HaveOccurred())
			d, err2 := oimcommon.ParseBDFString(testspdk.VHostDev)
			Expect(err2).NotTo(HaveOccurred())
			Expect(reply).To(Equal(&oim.MapVolumeReply{
				PciAddress: d,
				ScsiDisk:   &oim.SCSIDisk{},
			}))
			controllers, err := spdk.GetVHostControllers(ctx, c.SPDK)
			Expect(err).NotTo(HaveOccurred())
			Expect(controllers).To(HaveLen(1))
			Expect(controllers[0].Controller).To(Equal(testspdk.VHost))
			Expect(controllers[0].BackendSpecific).To(HaveKey("scsi"))
			scsi := controllers[0].BackendSpecific["scsi"].(spdk.SCSIControllerSpecific)
			Expect(scsi).To(HaveLen(1))
			Expect(scsi[0].SCSIDevNum).To(Equal(uint32(0)))
			Expect(scsi[0].LUNs).To(HaveLen(1))
			Expect(scsi[0].LUNs[0].BDevName).To(Equal(volumeID))

			return add, controllers
		}

		It("should have idempotent ProvisionMallocBDev", func() {
			_, err := c.ProvisionMallocBDev(context.Background(), &bdevArgs)
			Expect(err).NotTo(HaveOccurred())

			// Create new BDev.
			bdevArgs2 := oim.ProvisionMallocBDevRequest{
				BdevName: bdevName + "2",
				Size_:    1 * 1024 * 1024,
			}
			_, err = c.ProvisionMallocBDev(context.Background(), &bdevArgs2)
			Expect(err).NotTo(HaveOccurred())

			// Delete twice.
			bdevArgs2.Size_ = 0
			_, err = c.ProvisionMallocBDev(context.Background(), &bdevArgs2)
			Expect(err).NotTo(HaveOccurred())
			_, err = c.ProvisionMallocBDev(context.Background(), &bdevArgs2)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should work without QEMU", func() {
			var err error
			ctx := context.Background()

			By("mapping a volume")
			add, controllers := mapVolume()

			By("mapping again")
			_, err = c.MapVolume(context.Background(), &add)
			Expect(err).NotTo(HaveOccurred())
			controllers2, err := spdk.GetVHostControllers(ctx, c.SPDK)
			Expect(err).NotTo(HaveOccurred())
			Expect(controllers2).To(Equal(controllers))

			By("unmapping")
			remove := oim.UnmapVolumeRequest{
				VolumeId: "controller-test",
			}
			_, err = c.UnmapVolume(context.Background(), &remove)
			Expect(err).NotTo(HaveOccurred())

			By("unmapping twice")
			_, err = c.UnmapVolume(context.Background(), &remove)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with QEMU", func() {
			BeforeEach(func() {
				err := qemu.Init()
				Expect(err).NotTo(HaveOccurred())
				if qemu.VM == nil {
					Skip("No QEMU image.")
				}

				Eventually(func() (string, error) {
					return qemu.VM.SSH("lspci")
				}).Should(ContainSubstring("Virtio SCSI"))
			})

			AfterEach(func() {
				err := qemu.Finalize()
				Expect(err).NotTo(HaveOccurred())
			})

			It("should block device appear", func() {
				out, err := qemu.VM.SSH("lsblk")
				Expect(err).NotTo(HaveOccurred())
				Expect(out).NotTo(ContainSubstring("sda"))

				mapVolume()

				Eventually(func() (string, error) {
					return qemu.VM.SSH("lsblk")
				}).Should(ContainSubstring("sda"))

				// TODO: make this string configurable (https://github.com/spdk/spdk/issues/330)
				out, err = qemu.VM.SSH("cat", "/sys/block/sda/device/vendor")
				Expect(err).NotTo(HaveOccurred())
				Expect(out).To(Equal("INTEL   \n"))
			})
		})
	})
})
