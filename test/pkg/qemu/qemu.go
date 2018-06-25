/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

// qemu adds support for the TEST_QEMU_IMAGE env variable to test binaries
// and manages the virtual machine instance for tests.
package qemu

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/nightlyone/lockfile"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/qemu"
	"github.com/intel/oim/test/pkg/spdk"
)

var (
	VM *qemu.VM

	qemuImage = os.Getenv("TEST_QEMU_IMAGE")
	lock      *lockfile.Lockfile
)

// Init creates the virtual machine, if possible with VHost SCSI controller.
// Must be matched by a Finalize call, even after a failure.
func Init(logger oimcommon.SimpleLogger) error {
	if qemuImage == "" {
		return nil
	}

	// Protect against other processes using the same image.
	l, err := lockfile.New(qemuImage + ".testlock")
	if err == nil {
		delayed := false
		for {
			err = l.TryLock()
			if te, ok := err.(interface{ Temporary() bool }); !ok || !te.Temporary() {
				break
			}
			if !delayed {
				logger.Logf("Waiting for availability of %s", qemuImage)
				delayed = true
			}
			time.Sleep(time.Second)
		}
		if delayed {
			logger.Logf("Got access to %s", qemuImage)
		}
	}
	if err != nil {
		return fmt.Errorf("Locking %s.testlock: %s", qemuImage, err)
	}
	lock = &l

	opts := []string{}
	if spdk.SPDK != nil {
		// Run as explained in http://www.spdk.io/doc/vhost.html#vhost_qemu_config,
		// with a small memory size because we don't know how much huge pages
		// were set aside.
		opts = append(opts,
			"-object", "memory-backend-file,id=mem,size=1024M,mem-path=/dev/hugepages,share=on",
			"-numa", "node,memdev=mem",
			"-m", "1024",
			"-chardev", "socket,id=vhost0,path="+spdk.VHostPath,
			"-device", "vhost-user-scsi-pci,id=scsi0,chardev=vhost0,bus=pci.0,addr=0x15",
		)
	}
	logger.Logf("Starting %s with: %v", qemuImage, opts)
	vm, err := qemu.StartQEMU(qemuImage, opts...)
	if err != nil {
		return err
	}
	VM = vm

	kube := filepath.Join(filepath.Dir(qemuImage), "kube-"+filepath.Base(qemuImage))
	logger.Logf("Starting Kubernetes with: %s", kube)
	cmd := exec.Command(kube)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Starting Kubernetes with %s failed: %s\n%s", kube, err, out)
	}

	logger.Log("VM and Kubernetes ready.")
	return nil
}

// KubeConfig returns the full path for the Kubernetes cluster.
func KubeConfig() (string, error) {
	// Cluster is ready, treat it like a local cluster
	// (https://github.com/kubernetes/community/blob/master/contributors/devel/e2e-tests.md#bringing-up-a-cluster-for-testing).
	kubeconf, err := filepath.Abs(filepath.Join(filepath.Dir(qemuImage), filepath.Base(qemuImage)+"-kube.config"))
	if err != nil {
		return "", err
	}
	return kubeconf, nil
}

// Finalize frees any resources allocated by Init. Safe to call without
// Init or after Init failure.
func Finalize() error {
	// We must shut down QEMU first, otherwise
	// SPDK refuses to remove the controller.
	if VM != nil {
		VM.StopQEMU()
		VM = nil
	}
	if lock != nil {
		err := lock.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
