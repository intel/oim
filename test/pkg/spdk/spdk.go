/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

// spdk adds support for the TEST_SPDK_VHOST_SOCKET env variable to test binaries
// and manages the SPDK instance for tests.
package spdk

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nightlyone/lockfile"
	"github.com/pkg/errors"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/spdk"
	// . "github.com/onsi/ginkgo"
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
	spdkApp  = os.Getenv("TEST_SPDK_VHOST_BINARY")
	lock     *lockfile.Lockfile
	spdkCmd  *exec.Cmd
	tmpDir   string
	spdkOut  io.WriteCloser
)

// Init connects to SPDK and creates a VHost SCSI controller.
// Must be matched by a Finalize call, even after a failure.
func Init(logger oimcommon.SimpleLogger, controller bool) error {
	// Set up VHost SCSI, if we have SPDK.
	if spdkSock == "" && spdkApp == "" {
		return nil
	}

	if SPDK != nil || VHostPath != "" || spdkCmd != nil {
		return errors.New("Finalize not called or failed")
	}

	if spdkApp != "" {
		// TODO: suppress logging to syslog
		if t, err := ioutil.TempDir("", "spdk"); err != nil {
			return errors.Wrap(err, "SPDK temp directory")
		} else {
			tmpDir = t
		}
		spdkSock = filepath.Join(tmpDir, "spdk.sock")
		spdkOut = oimcommon.LogWriter(logger, "spdk: ")
		{
			logger.Logf("Starting %s", spdkApp)
			cmd := exec.Command("sudo", spdkApp, "-S", tmpDir, "-r", spdkSock)
			// Start with its own process group so that we can kill sudo
			// and its child spdkApp via the process group.
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Stdout = spdkOut
			cmd.Stderr = spdkOut
			if err := cmd.Start(); err != nil {
				return err
			}
			spdkCmd = cmd
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		for {
			if ctx.Err() != nil {
				return fmt.Errorf("Timed out waiting for %s", spdkSock)
			}
			_, err := os.Stat(spdkSock)
			if err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		{
			cmd := exec.CommandContext(ctx, "sudo", "chmod", "a+rw", spdkSock)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return errors.Wrapf(err, "chmod %s: %s", spdkSock, out)
			}
		}

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
			if err := spdk.RemoveVHostController(context.Background(), SPDK, args); err != nil {
				return err
			}
			VHostPath = ""
		}
		SPDK.Close()
		SPDK = nil
	}
	if spdkCmd != nil {
		// Kill the process group to catch both child (sudo) and grandchild (SPDK).
		timer := time.AfterFunc(10*time.Second, func() {
			exec.Command("sudo", "--non-interactive", "kill", "-9", fmt.Sprintf("-%d", spdkCmd.Process.Pid)).CombinedOutput()
		})
		defer timer.Stop()
		exec.Command("sudo", "--non-interactive", "kill", fmt.Sprintf("-%d", spdkCmd.Process.Pid)).CombinedOutput()
		spdkCmd.Wait()
		spdkCmd = nil
	}
	if lock != nil {
		if err := lock.Unlock(); err != nil {
			return err
		}
	}
	if spdkOut != nil {
		if err := spdkOut.Close(); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(tmpDir); err != nil {
		return err
	}
	return nil
}
