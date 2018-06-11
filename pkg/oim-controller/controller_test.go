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
	"path/filepath"

	"github.com/intel/oim/pkg/oim-controller"
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
		vhost     = "vhost.0"
		vhostPath string
	)

	BeforeEach(func() {
		// We create the VHost Controller below. Here we
		// just construct the path and set it.
		vhostPath = filepath.Join(filepath.Dir(spdkPath), vhost)
		var err error

		c, err = oimcontroller.New(oimcontroller.OptionSPDK(spdkPath),
			oimcontroller.OptionVHostController(vhostPath))
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("attaching a volume", func() {
		It("should fail when missing parameters", func() {
			request := oim.MapVolumeRequest{
				UUID: "foobar",
			}
			_, err := c.MapVolume(context.Background(), &request)
			Expect(err).To(HaveOccurred())
		})

		Context("with SPDK", func() {
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
			})

			AfterEach(func() {
				// Clean up all bdevs and thus also VHost LUNs which might
				// have been created during testing.
				failed := []error{}

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

				Expect(failed).Should(BeEmpty())
			})

			It("should work without QEMU", func() {
				request := oim.MapVolumeRequest{
					UUID: "controller-test",
					Params: &oim.MapVolumeRequest_Malloc{
						Malloc: &oim.MallocParams{
							Size: 1 * 1024 * 1024,
						},
					},
				}
				_, err := c.MapVolume(context.Background(), &request)
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

					var err error
					vm, err = qemu.StartQEMU()
					Expect(err).NotTo(HaveOccurred())
				})

				AfterEach(func() {
					err := vm.StopQEMU()
					Expect(err).NotTo(HaveOccurred())
				})

				It("should block device appear", func() {
					// TODO: repeat MapVolumeRequest and check that the VM
					// guest detects the block device.
				})
			})
		})
	})
})
