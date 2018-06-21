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
	"os/exec"
	"path/filepath"
	"time"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-controller"
	"github.com/intel/oim/pkg/oim-registry"
	"github.com/intel/oim/pkg/qemu"
	"github.com/intel/oim/pkg/spdk"
	"github.com/intel/oim/pkg/spec/oim/v0"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("OIM Controller", func() {
	var (
		c         *oimcontroller.Controller
		spdkPath  = os.Getenv("TEST_SPDK_VHOST_SOCKET")
		vhost     = "controller-test-vhost"
		vhostPath string
		// Bus, address, and device string must match.
		vhostBus  = "pci.0"
		vhostAddr = 0x15
		vhostDev  = fmt.Sprintf("/devices/pci0000:00/0000:00:%x.0/", vhostAddr)
	)

	BeforeEach(func() {
		// We create the VHost Controller below. Here we
		// just construct the path and set it.
		vhostPath = filepath.Join(filepath.Dir(spdkPath), vhost)
		var err error

		c, err = oimcontroller.New(oimcontroller.WithSPDK(spdkPath),
			oimcontroller.WithVHostDev(vhostDev),
			oimcontroller.WithVHostController(vhostPath))
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

			// Spin up registry.
			db = &oimregistry.MemRegistryDB{}
			registry, err = oimregistry.New(oimregistry.DB(db))
			Expect(err).NotTo(HaveOccurred())
			registryServer, service := oimregistry.Server("tcp4://:0", registry)
			err = registryServer.Start(service)
			Expect(err).NotTo(HaveOccurred())
			addr := registryServer.Addr()
			Expect(addr).NotTo(BeNil())
			// No tcp4:/// prefix. It causes gRPC to block?!
			registryAddress = addr.String()
		})

		AfterEach(func() {
			if registryServer != nil {
				registryServer.ForceStop()
				registryServer.Wait()
			}
		})

		It("should work", func() {
			addr := "foo://bar"
			controllerID := "controller-registration-test-1"
			c, err := oimcontroller.New(
				oimcontroller.WithRegistry(registryAddress),
				oimcontroller.WithControllerID(controllerID),
				oimcontroller.WithControllerAddress(addr),
			)
			err = c.Start()
			Expect(err).NotTo(HaveOccurred())
			defer c.Stop()

			Eventually(func() oimregistry.MemRegistryDB {
				return *db
			}).Should(Equal(oimregistry.MemRegistryDB{controllerID: addr}))
		})

		It("should re-register", func() {
			addr := "foo://bar"
			controllerID := "controller-registration-test-2"
			c, err := oimcontroller.New(
				oimcontroller.WithRegistry(registryAddress),
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
			Eventually(getDB, 1*time.Second).Should(Equal(oimregistry.MemRegistryDB{controllerID: addr}))
			(*db)[controllerID] = ""
			Consistently(getDB, 4*time.Second).Should(Equal(oimregistry.MemRegistryDB{controllerID: ""}))
			Eventually(getDB, 120*time.Second).Should(Equal(oimregistry.MemRegistryDB{controllerID: addr}))
		})

		It("should really stop", func() {
			addr := "foo://bar"
			controllerID := "controller-registration-test-3"
			c, err := oimcontroller.New(
				oimcontroller.WithRegistry(registryAddress),
				oimcontroller.WithControllerID(controllerID),
				oimcontroller.WithControllerAddress(addr),
				oimcontroller.WithRegistryDelay(5*time.Second),
			)
			err = c.Start()
			Expect(err).NotTo(HaveOccurred())

			getDB := func() oimregistry.MemRegistryDB {
				return *db
			}
			Eventually(getDB, 1*time.Second).Should(Equal(oimregistry.MemRegistryDB{controllerID: addr}))
			c.Stop()
			(*db)[controllerID] = ""
			Consistently(getDB, 10*time.Second).Should(Equal(oimregistry.MemRegistryDB{controllerID: ""}))
		})
	})

	Describe("attaching a volume", func() {
		It("should fail when missing parameters", func() {
			request := oim.MapVolumeRequest{
				VolumeId: "foobar",
			}
			_, err := c.MapVolume(context.Background(), &request)
			Expect(err).To(HaveOccurred())
		})

		Context("with SPDK", func() {
			var (
				// Names must match for MapVolume to succeed.
				volumeID = "controller-test"
				bdevName = volumeID
				bdevArgs = oim.ProvisionMallocBDevRequest{
					BdevName: bdevName,
					Size:     1 * 1024 * 1024,
				}
			)

			BeforeEach(func() {
				if spdkPath == "" {
					Skip("No SPDK vhost, TEST_SPDK_VHOST_SOCKET is empty.")
				}

				// Create a new VHost controller.
				args := spdk.ConstructVHostSCSIControllerArgs{
					Controller: vhost,
				}
				err := spdk.ConstructVHostSCSIController(context.Background(), c.SPDK, args)
				Expect(err).NotTo(HaveOccurred())
				Expect(vhostPath).To(BeAnExistingFile())

				// If we are not running as root, we
				// need to change permissions on the
				// new socket.
				if os.Getuid() != 0 {
					cmd := exec.Command("sudo", "chmod", "a+rw", vhostPath)
					out, err := cmd.CombinedOutput()
					Expect(err).NotTo(HaveOccurred(), "'sudo chmod' output: %s", string(out))
				}

				_, err = c.ProvisionMallocBDev(context.Background(), &bdevArgs)
				Expect(err).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				// Clean up all bdevs and thus also VHost LUNs which might
				// have been created during testing.
				failed := []error{}

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

				// And also the VHost controller.
				args := spdk.RemoveVHostControllerArgs{
					Controller: vhost,
				}
				if err := spdk.RemoveVHostController(context.Background(), c.SPDK, args); err != nil {
					failed = append(failed, errors.New(fmt.Sprintf("RemoveVHostController %s: %s", vhost, err)))
				}

				Expect(failed).To(BeEmpty())
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
				reply, err := c.MapVolume(context.Background(), &add)
				Expect(err).NotTo(HaveOccurred())
				Expect(reply).To(Equal(&oim.MapVolumeReply{
					Device: vhostDev,
					Scsi:   "0:0",
				}))

				controllers, err := spdk.GetVHostControllers(ctx, c.SPDK)
				Expect(err).NotTo(HaveOccurred())
				Expect(controllers).To(HaveLen(1))
				Expect(controllers[0].Controller).To(Equal(vhost))
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
					Size:     1 * 1024 * 1024,
				}
				_, err = c.ProvisionMallocBDev(context.Background(), &bdevArgs2)
				Expect(err).NotTo(HaveOccurred())

				// Delete twice.
				bdevArgs2.Size = 0
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
				var (
					vm *qemu.VM
				)

				BeforeEach(func() {
					if image := os.Getenv("TEST_QEMU_IMAGE"); image == "" {
						Skip("No QEMU configured via TEST_QEMU_IMAGE")
					}

					// Run as explained in http://www.spdk.io/doc/vhost.html#vhost_qemu_config,
					// with a small memory size because we don't know how much huge pages
					// were set aside.
					var err error
					vm, err = qemu.StartQEMU(
						"-object", "memory-backend-file,id=mem,size=256M,mem-path=/dev/hugepages,share=on",
						"-numa", "node,memdev=mem",
						"-m", "256",
						"-chardev", "socket,id=vhost0,path="+vhostPath,
						"-device", fmt.Sprintf("vhost-user-scsi-pci,id=scsi0,chardev=vhost0,bus=%s,addr=0x%x", vhostBus, vhostAddr))
					Expect(err).NotTo(HaveOccurred())

					Eventually(func() (string, error) {
						return vm.SSH("lspci")
					}).Should(ContainSubstring("Virtio SCSI"))
				})

				AfterEach(func() {
					err := vm.StopQEMU()
					Expect(err).NotTo(HaveOccurred())
				})

				It("should block device appear", func() {
					out, err := vm.SSH("lsblk")
					Expect(err).NotTo(HaveOccurred())
					Expect(out).NotTo(ContainSubstring("sda"))

					mapVolume()

					Eventually(func() (string, error) {
						return vm.SSH("lsblk")
					}).Should(ContainSubstring("sda"))

					// TODO: make this string configurable (https://github.com/spdk/spdk/issues/330)
					out, err = vm.SSH("cat", "/sys/block/sda/device/vendor")
					Expect(err).NotTo(HaveOccurred())
					Expect(out).To(Equal("INTEL   \n"))
				})
			})
		})
	})
})
