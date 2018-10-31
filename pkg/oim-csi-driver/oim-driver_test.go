/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"context"
	"fmt"
	"github.com/kubernetes-csi/csi-test/pkg/sanity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/log/testlog"
	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/oim-controller"
	"github.com/intel/oim/pkg/oim-registry"
	"github.com/intel/oim/pkg/spec/oim/v0"
	"github.com/intel/oim/test/pkg/spdk"

	. "github.com/onsi/ginkgo"
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
//
// The corresponding test for non-local mode is in
// test/e2e/storage/oim-csi.go.
func TestSPDK(t *testing.T) {
	// The sanity suite uses Ginkgo, so log via that.
	log.SetOutput(GinkgoWriter)
	ctx := context.Background()

	defer spdk.Finalize()
	if err := spdk.Init(); err != nil {
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
	s, err := driver.Start(ctx)
	defer s.ForceStop(ctx)

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
		PciAddress: &oim.PCIAddress{
			Bus:    8,
			Device: 7,
		},
		ScsiDisk: &oim.SCSIDisk{},
	}, nil
}

func (m *MockController) UnmapVolume(ctx context.Context, in *oim.UnmapVolumeRequest) (*oim.UnmapVolumeReply, error) {
	return &oim.UnmapVolumeReply{}, nil
}

func (m *MockController) ProvisionMallocBDev(ctx context.Context, in *oim.ProvisionMallocBDevRequest) (*oim.ProvisionMallocBDevReply, error) {
	return &oim.ProvisionMallocBDevReply{}, nil
}

func (m *MockController) CheckMallocBDev(ctx context.Context, in *oim.CheckMallocBDevRequest) (*oim.CheckMallocBDevReply, error) {
	return &oim.CheckMallocBDevReply{}, nil
}

// Runs tests with OIM registry and a mock controller.
// This can only be used to test the communication paths, but not
// the actual operation.
func TestMockOIM(t *testing.T) {
	defer testlog.SetGlobal(t)()
	ctx := context.Background()
	adminCtx := oimregistry.RegistryClientContext(ctx, "user.admin")
	var err error

	tmp, err := ioutil.TempDir("", "oim-driver")
	require.NoError(t, err)
	defer os.RemoveAll(tmp)

	controllerID := "host-0"

	registryAddress := "unix://" + tmp + "/oim-registry.sock"
	tlsConfig, err := oimcommon.LoadTLSConfig(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"), os.ExpandEnv("${TEST_WORK}/ca/component.registry.key"), "")
	require.NoError(t, err)
	registry, err := oimregistry.New(oimregistry.TLS(tlsConfig))
	require.NoError(t, err)
	registryServer, service := registry.Server(registryAddress)
	err = registryServer.Start(ctx, service)
	require.NoError(t, err)
	defer registryServer.ForceStop(ctx)

	controllerAddress := "unix://" + tmp + "/oim-controller.sock"
	controller := &MockController{}
	require.NoError(t, err)
	controllerCreds, err := oimcommon.LoadTLS(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"),
		os.ExpandEnv("${TEST_WORK}/ca/controller."+controllerID),
		"component.registry")
	require.NoError(t, err)
	controllerServer, controllerService := oimcontroller.Server(controllerAddress, controller, controllerCreds)
	err = controllerServer.Start(ctx, controllerService)
	require.NoError(t, err)
	defer controllerServer.ForceStop(ctx)

	_, err = registry.SetValue(adminCtx, &oim.SetValueRequest{
		Value: &oim.Value{
			Path:  controllerID + "/" + oimcommon.RegistryAddress,
			Value: controllerAddress,
		},
	})
	require.NoError(t, err)

	endpoint := "unix://" + tmp + "/oim-driver.sock"
	driver, err := New(WithCSIEndpoint(endpoint),
		WithOIMRegistryAddress(registryAddress),
		WithRegistryCreds(os.ExpandEnv("${TEST_WORK}/ca/ca.crt"), os.ExpandEnv("${TEST_WORK}/ca/host."+controllerID)),
		WithOIMControllerID(controllerID),
	)
	require.NoError(t, err)
	s, err := driver.Start(ctx)
	defer s.ForceStop(ctx)

	// CSI does not use transport security for its Unix domain socket.
	opts := oimcommon.ChooseDialOpts(endpoint, grpc.WithBlock(), grpc.WithInsecure())
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
		// Both gRPC and waitForDevice will abort when the deadline is reached.
		// Where the expiration is detected first is random, so the exact error
		// message can vary.
		//
		// What we can test reliably is that we get a DeadlineExceeded gRPC code.
		assert.Equal(t, status.Convert(err).Code(), codes.DeadlineExceeded, fmt.Sprintf("expected DeadlineExceeded, got: %s", err))
	}
}
