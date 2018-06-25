/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

// spdk adds support for the TEST_SPDK_VHOST_SOCKET env variable to test binaries
// and manages the SPDK instance for tests.
package spdk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/nightlyone/lockfile"

	"github.com/intel/oim/pkg/spdk"
)

var (
	// Connected to the running SPDK.
	SPDK *spdk.Client
	// Path to the socket of the running SPDK.
	SPDKPath string
	// Path to the SCSI VHost controller of the running SPDK.
	VHostPath string

	// Controller name.
	VHost = "e2e-test-vhost"

	// Bus, address, and device string must match.
	VHostBus  = "pci.0"
	VHostAddr = 0x15
	VHostDev  = fmt.Sprintf("/devices/pci0000:00/0000:00:%x.0/", VHostAddr)

	spdkSock = os.Getenv("TEST_SPDK_VHOST_SOCKET")
	lock     *lockfile.Lockfile
)

// Init connects to SPDK and creates a VHost SCSI controller.
// Must be matched by a Finalize call, even after a failure.
func Init(logger oimcommon.SimpleLogger, controller bool) error {
	// Set up VHost SCSI, if we have SPDK.
	if spdkSock == "" && spdkApp == "" {
		return nil
	}

	if SPDK != nil || VHostPath != "" {
		return errors.New("Finalize not called or failed")
	}

	// Protect against other processes using the same daemon.
	l, err := lockfile.New(spdkSock + ".testlock")
	if err == nil {
		for {
			err = l.TryLock()
			if te, ok := err.(interface{ Temporary() bool }); !ok || !te.Temporary() {
				break
			}
			time.Sleep(time.Second)
		}
	}
	if err != nil {
		return fmt.Errorf("Locking %s.testlock: %s", spdkSock, err)
	}
	lock = &l

	s, err := spdk.New(spdkSock)
	if err != nil {
		return err
	}
	SPDK = s
	SPDKPath = spdkSock
	if controller {
		args := spdk.ConstructVHostSCSIControllerArgs{
			Controller: VHost,
		}
		err = spdk.ConstructVHostSCSIController(context.Background(), SPDK, args)
		if err != nil {
			return err
		}
		VHostPath = filepath.Join(filepath.Dir(spdkSock), VHost)

		// If we are not running as root, we need to
		// change permissions on the new socket.
		if os.Getuid() != 0 {
			cmd := exec.Command("sudo", "chmod", "a+rw", VHostPath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("'sudo chmod' on vhost socket %s failed: %s\n%s", VHostPath, err, string(out))
			}
		}
	} else {
		VHostPath = ""
	}

	return nil
}

// Finalize frees any resources allocated by Init. Safe to call without
// Init or after Init failure.
func Finalize() error {
	if SPDK != nil {
		if VHostPath != "" {
			args := spdk.RemoveVHostControllerArgs{
				Controller: VHost,
			}
			err := spdk.RemoveVHostController(context.Background(), SPDK, args)
			if err != nil {
				return err
			}
			VHostPath = ""
		}
		SPDK.Close()
		SPDK = nil
	}
	if lock != nil {
		err := lock.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
