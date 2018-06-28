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

	"github.com/intel/oim/test/pkg/spdk"
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
	defer spdk.Finalize()
	if err := spdk.Init(t, false); err != nil {
		require.NoError(t, err)
	}
	if spdk.SPDK == nil {
		t.Skip("No VHost.")
	}

	tmp, err := ioutil.TempDir("", "oim-driver")
	require.NoError(t, err)
	defer os.RemoveAll(tmp)

	endpoint := "unix://" + tmp + "/oim-driver.sock"
	driver, err := New(WithCSIEndpoint(endpoint), WithVHostEndpoint(spdk.SPDKPath))
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
