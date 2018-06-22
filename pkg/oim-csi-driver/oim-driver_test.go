/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kubernetes-csi/csi-test/pkg/sanity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"

	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-controller"
	"github.com/intel/oim/pkg/oim-registry"
	"github.com/intel/oim/pkg/spec/oim/v0"
)

// SudoMount provides wrappers around several commands used by the k8s
// mount utility code. It then runs those commands under pseudo. This
// allows building and running tests as normal users.
type SudoMount struct {
	tmpDir     string
	searchPath string
}

func SetupSudoMount(t *testing.T) SudoMount {
	tmpDir, err := ioutil.TempDir("", "sanity-node")
	require.NoError(t, err)
	s := SudoMount{
		tmpDir:     tmpDir,
		searchPath: os.Getenv("PATH"),
	}
	for _, cmd := range []string{"mount", "umount", "blkid", "fsck", "mkfs.ext2", "mkfs.ext3", "mkfs.ext4"} {
		wrapper := filepath.Join(s.tmpDir, cmd)
		content := fmt.Sprintf(`#!/bin/sh
PATH=%q
if [ $(id -u) != 0 ]; then
   exec sudo %s "$@"
else
   exec %s "$@"
fi
`, s.searchPath, cmd, cmd)
		err := ioutil.WriteFile(wrapper, []byte(content), 0777)
		require.NoError(t, err)
	}
	os.Setenv("PATH", tmpDir+":"+s.searchPath)
	return s
}

func (s SudoMount) Close() {
	os.RemoveAll(s.tmpDir)
	os.Setenv("PATH", s.searchPath)
}

// Runs tests in local SPDK mode.
func TestSPDK(t *testing.T) {
	vhost := os.Getenv("TEST_SPDK_VHOST_SOCKET")
	if vhost == "" {
		t.Skip("No SPDK vhost, TEST_SPDK_VHOST_SOCKET is empty.")
	}

	tmp, err := ioutil.TempDir("", "oim-driver")
	require.NoError(t, err)
	defer os.RemoveAll(tmp)

	endpoint := "unix://" + tmp + "/oim-driver.sock"
	driver, err := New(WithCSIEndpoint(endpoint), WithVHostEndpoint(vhost))
	require.NoError(t, err)
	s, err := driver.Start()
	defer s.ForceStop()

	sudo := SetupSudoMount(t)
	defer sudo.Close()

	// Now call the test suite.
	config := sanity.Config{
		TargetPath:     tmp + "/target-path",
		StagingPath:    tmp + "/staging-path",
		Address:        endpoint,
		TestVolumeSize: 1 * 1024 * 1024,
	}
	sanity.Test(t, &config)
}

// MockController implements oim.Controller.
type MockController struct {
	MapVolumes   []oim.MapVolumeRequest
	UnmapVolumes []oim.UnmapVolumeRequest
}

func (m *MockController) MapVolume(ctx context.Context, in *oim.MapVolumeRequest) (*oim.MapVolumeReply, error) {
	m.MapVolumes = append(m.MapVolumes, *in)
	return &oim.MapVolumeReply{
		Device: "this-is-not-the-device-you-are-looking-for",
		Scsi:   "0:0",
	}, nil
}

func (m *MockController) UnmapVolume(ctx context.Context, in *oim.UnmapVolumeRequest) (*oim.UnmapVolumeReply, error) {
	return &oim.UnmapVolumeReply{}, nil
}

func (m *MockController) ProvisionMallocBDev(ctx context.Context, in *oim.ProvisionMallocBDevRequest) (*oim.ProvisionMallocBDevReply, error) {
	return &oim.ProvisionMallocBDevReply{}, nil
}

// Runs tests with OIM registry and a mock controller.
// This can only be used to test the communication paths, but not
// the actual operation.
func TestMockOIM(t *testing.T) {
	ctx := context.Background()
	var err error

	tmp, err := ioutil.TempDir("", "oim-driver")
	require.NoError(t, err)
	defer os.RemoveAll(tmp)

	controllerID := "my-test-controller-ID"

	registryAddress := "unix://" + tmp + "/oim-registry.sock"
	registry, err := oimregistry.New()
	require.NoError(t, err)
	registryServer, service := oimregistry.Server(registryAddress, registry)
	err = registryServer.Start(service)
	require.NoError(t, err)
	defer registryServer.ForceStop()

	controllerAddress := "unix://" + tmp + "/oim-controller.sock"
	controller := &MockController{}
	require.NoError(t, err)
	controllerServer, controllerService := oimcontroller.Server(controllerAddress, controller)
	err = controllerServer.Start(controllerService)
	require.NoError(t, err)
	defer controllerServer.ForceStop()

	_, err = registry.RegisterController(ctx, &oim.RegisterControllerRequest{
		ControllerId: controllerID,
		Address:      controllerAddress,
	})
	require.NoError(t, err)

	endpoint := "unix://" + tmp + "/oim-driver.sock"
	driver, err := New(WithCSIEndpoint(endpoint),
		WithOIMRegistryAddress(registryAddress),
		WithOIMControllerID(controllerID),
	)
	require.NoError(t, err)
	s, err := driver.Start()
	defer s.ForceStop()

	opts := oimcommon.ChooseDialOpts(endpoint, grpc.WithBlock())
	conn, err := grpc.Dial(endpoint, opts...)
	require.NoError(t, err)
	csiClient := csi.NewNodeClient(conn)

	// This will start waiting for a device that can never appear,
	// so we force it to time out.
	volumeID := "my-test-volume"
	deadline, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, err = csiClient.NodePublishVolume(deadline,
		&csi.NodePublishVolumeRequest{
			VolumeId:         volumeID,
			TargetPath:       tmp + "/target",
			VolumeCapability: &csi.VolumeCapability{},
		})
	if assert.Error(t, err) {
		assert.Equal(t, "rpc error: code = DeadlineExceeded desc = timed out waiting for device 'this-is-not-the-device-you-are-looking-for', SCSI unit '0:0'", err.Error())
	}
}

func TestFindDev(t *testing.T) {
	var err error
	var dev string
	var major, minor int

	tmp, err := ioutil.TempDir("", "find-dev")
	require.NoError(t, err)
	defer os.RemoveAll(tmp)

	// Nothing in empty dir.
	dev, major, minor, err = findDev(tmp, "foo", "0:0")
	assert.NoError(t, err)
	assert.Equal(t, "", dev)

	// Neither in a populated one.
	entries := map[string]string{
		"11:0":  "../../devices/pci0000:00/0000:00:14.0/usb1/1-3/1-3.4/1-3.4.1/1-3.4.1:1.0/host5/target5:0:0/5:0:0:0/block/sr0",
		"254:0": "../../devices/virtual/block/dm-0",
		"254:1": "../../devices/virtual/block/dm-1",
		"254:2": "../../devices/virtual/block/dm-2",
		"254:3": "../../devices/virtual/block/dm-3",
		"254:4": "../../devices/virtual/block/dm-4",
		"7:0":   "../../devices/virtual/block/loop0",
		"7:1":   "../../devices/virtual/block/loop1",
		"7:2":   "../../devices/virtual/block/loop2",
		"7:3":   "../../devices/virtual/block/loop3",
		"7:4":   "../../devices/virtual/block/loop4",
		"7:5":   "../../devices/virtual/block/loop5",
		"7:6":   "../../devices/virtual/block/loop6",
		"7:7":   "../../devices/virtual/block/loop7",
		"8:0":   "../../devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0/block/sda",
		"8:1":   "../../devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda1",
		"8:16":  "../../devices/pci0000:00/0000:00:14.0/usb2/2-4/2-4.3/2-4.3.4/2-4.3.4.1/2-4.3.4.1:1.0/host4/target4:0:0/4:0:0:0/block/sdb",
		"8:2":   "../../devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda2",
		"8:3":   "../../devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda3",
		"8:5":   "../../devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0/block/sda/sda5",
	}
	for from, to := range entries {
		err = os.Symlink(to, filepath.Join(tmp, from))
		require.NoError(t, err)
	}
	dev, major, minor, err = findDev(tmp, "foo", "0:0")
	assert.NoError(t, err)
	assert.Equal(t, "", dev)

	// Closer, but not quite.
	dev, major, minor, err = findDev(tmp, "/devices/pci0000:00/0000:00:17.0/", "5:0")
	assert.NoError(t, err)
	assert.Equal(t, "", dev)

	// Find sda.
	dev, major, minor, err = findDev(tmp, "/devices/pci0000:00/0000:00:17.0/", "0:0")
	assert.NoError(t, err)
	assert.Equal(t, "sda", dev)
	assert.Equal(t, major, 8)
	assert.Equal(t, minor, 0)

	// Without SCSI.
	dev, major, minor, err = findDev(tmp, "/devices/pci0000:00/0000:00:18.0/", "")
	assert.NoError(t, err)
	assert.Equal(t, "", dev)

	// Find sda.
	dev, major, minor, err = findDev(tmp, "/devices/pci0000:00/0000:00:17.0/", "")
	assert.NoError(t, err)
	assert.Equal(t, "sda", dev)

	// No deadline.
	dev, major, minor, err = waitForDevice(context.Background(), tmp, "/devices/pci0000:00/0000:00:17.0/", "0:0")
	assert.NoError(t, err)
	assert.Equal(t, "sda", dev)
	assert.Equal(t, major, 8)
	assert.Equal(t, minor, 0)

	// Timeout aborts wait.
	timeout, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	dev, major, minor, err = waitForDevice(timeout, tmp, "/devices/pci0000:00/0000:00:17.0/", "1:0")
	assert.Error(t, err)
	assert.Equal(t, "rpc error: code = DeadlineExceeded desc = timed out waiting for device '/devices/pci0000:00/0000:00:17.0/', SCSI unit '1:0'", err.Error())

	// Create the expected entry in two seconds, wait at most five.
	timeout2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	timer := time.AfterFunc(2*time.Second, func() {
		err = os.Symlink("../../devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:1/0:0:1:0/block/sdc", filepath.Join(tmp, "9:0"))
		require.NoError(t, err)
	})
	defer timer.Stop()
	dev, major, minor, err = waitForDevice(timeout2, tmp, "/devices/pci0000:00/0000:00:17.0/", "1:0")
	assert.NoError(t, err)
	assert.Equal(t, "sdc", dev)
	assert.Equal(t, major, 9)
	assert.Equal(t, minor, 0)

	// Broken entry.
	err = os.Symlink("../../devices/pci0000:00/0000:00:17.0/ata1/host0/target0:0:1/0:0:2:0/block/sdd", filepath.Join(tmp, "a:b"))
	require.NoError(t, err)
	dev, major, minor, err = findDev(tmp, "/devices/pci0000:00/0000:00:17.0/", "2:0")
	if assert.Error(t, err) {
		assert.Equal(t, fmt.Sprintf("Unexpected entry in %s, not a major:minor symlink: a:b", tmp), err.Error())
	}
}
