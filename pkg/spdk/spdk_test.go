/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package spdk_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intel/oim/pkg/log/testlog"
	"github.com/intel/oim/pkg/oim-common"
	"github.com/intel/oim/pkg/spdk"
	testspdk "github.com/intel/oim/test/pkg/spdk"
)

func connect(t *testing.T) *spdk.Client {
	var err error

	err = testspdk.Init()
	require.NoError(t, err)
	if testspdk.SPDK == nil {
		t.Skip("No SPDK vhost.")
	}
	return testspdk.SPDK
}

func TestGetBDevs(t *testing.T) {
	defer testlog.SetGlobal(t)()
	defer testspdk.Finalize()
	client := connect(t)
	response, err := spdk.GetBDevs(context.Background(), client, spdk.GetBDevsArgs{})
	assert.NoError(t, err, "Failed to list bdevs: %s", err)
	assert.Empty(t, response, "Unexpected non-empty bdev list")
	testspdk.Finalize()
}

func TestError(t *testing.T) {
	defer testlog.SetGlobal(t)()
	defer testspdk.Finalize()
	client := connect(t)

	// It would be nice to get a well-documented error code here,
	// but currently we don't (https://github.com/spdk/spdk/issues/319).
	_, err := spdk.GetBDevs(context.Background(), client, spdk.GetBDevsArgs{Name: "no-such-bdev"})
	require.Error(t, err, "Should have failed to find no-such-bdev")
	require.True(t, spdk.IsJSONError(err, spdk.ERROR_INVALID_PARAMS), "IsJSONError(%+v, ERROR_INVALID_PARAMS)", err)
}

func TestMallocBDev(t *testing.T) {
	defer testlog.SetGlobal(t)()
	ctx := context.Background()
	defer testspdk.Finalize()
	client := connect(t)

	var created spdk.ConstructBDevResponse
	cleanup := func(when string) {
		t.Logf("Cleaning up at %s: %+v", when, created)
		if created != "" {
			err := spdk.DeleteBDev(ctx, client, spdk.DeleteBDevArgs{Name: string(created)})
			require.NoError(t, err, "Failed to delete bdev %s: %s", created)
			created = ""
		}
	}
	defer cleanup("deferred cleanup")

	// 1MB seems to be the minimum size?
	for i, arg := range []spdk.ConstructMallocBDevArgs{
		spdk.ConstructMallocBDevArgs{ConstructBDevArgs: spdk.ConstructBDevArgs{NumBlocks: 2048, BlockSize: 512}},
		spdk.ConstructMallocBDevArgs{ConstructBDevArgs: spdk.ConstructBDevArgs{NumBlocks: 4096, BlockSize: 512, Name: "MyMallocBdev", UUID: "11111111-2222-3333-4444-555555555555"}},
	} {
		cleanup(fmt.Sprintf("bdev %d", i))
		// Can't use := here, it would shadow "created"!
		var err error
		created, err = spdk.ConstructMallocBDev(ctx, client, arg)
		if !assert.NoError(t, err, "Failed to create %+v", arg) {
			continue
		}
		t.Logf("Created %+v", created)
		if arg.Name != "" {
			assert.Equal(t, arg.Name, string(created), "choosen name")
		}
		bdevs, err := spdk.GetBDevs(ctx, client, spdk.GetBDevsArgs{Name: string(created)})
		t.Logf("bdev %s attributes: %+v", created, bdevs)
		if !assert.NoError(t, err, "Failed to retrieve bdev %s attributes", created) {
			continue
		}
		if len(bdevs) != 1 {
			t.Errorf("Should have received exactly one bdev")
			continue
		}
		bdev := bdevs[0]
		expected := spdk.BDev{
			Name:        string(created),
			ProductName: "Malloc disk",
			BlockSize:   arg.BlockSize,
			NumBlocks:   arg.NumBlocks,
			UUID:        arg.UUID,
			SupportedIOTypes: spdk.SupportedIOTypes{
				Read:       true,
				Write:      true,
				Unmap:      true,
				WriteZeros: true,
				Flush:      true,
				Reset:      true,
			}}
		if arg.UUID == "" {
			expected.UUID = bdev.UUID
		}
		assert.Equal(t, expected, bdev)
	}
}

func TestNBDDev(t *testing.T) {
	defer testlog.SetGlobal(t)()
	ctx := context.Background()
	defer testspdk.Finalize()
	client := connect(t)

	name := "my_malloc_bdev"
	numBlocks := int64(2048)
	blockSize := int64(512)
	createArg := spdk.ConstructMallocBDevArgs{ConstructBDevArgs: spdk.ConstructBDevArgs{NumBlocks: numBlocks, BlockSize: blockSize, Name: name}}
	// TODO: this does not get called when the test fails?
	defer func() {
		spdk.DeleteBDev(ctx, client, spdk.DeleteBDevArgs{Name: name})
	}()
	_, err := spdk.ConstructMallocBDev(ctx, client, createArg)
	require.NoError(t, err, "Failed to create %+v", createArg)

	nbd, err := spdk.GetNBDDisks(ctx, client)
	assert.NoError(t, err, "get initial list of disks")

	// Find the first unused nbd device node. Unfortunately
	// this is racy.
	var nbdDevice string
	var nbdFile *os.File
	for i := 0; ; i++ {
		nbdDevice = fmt.Sprintf("/dev/nbd%d", i)
		nbdFile, err = os.Open(nbdDevice)
		require.NoError(t, err)
		defer nbdFile.Close()
		size, err := oimcommon.GetBlkSize64(nbdFile)
		require.NoError(t, err)
		if size == 0 {
			break
		} else {
			nbdFile.Close()
		}
	}
	t.Logf("Using NBD %s, %+v", nbdDevice, nbdFile)

	startArg := spdk.StartNBDDiskArgs{BDevName: name, NBDDevice: nbdDevice}
	err = spdk.StartNBDDisk(ctx, client, startArg)
	require.NoError(t, err, "Start NBD Disk with %+v", startArg)

	nbd, err = spdk.GetNBDDisks(ctx, client)
	assert.NoError(t, err, "get initial list of disks")
	assert.Equal(t, nbd, spdk.GetNBDDisksResponse{startArg}, "should have one NBD device running")

	// There's a slight race here between the kernel noticing the new size
	// and us checking for it.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
loop:
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for nbd %s to get populated.", nbdDevice)
		case <-time.After(time.Millisecond):
			size, err := oimcommon.GetBlkSize64(nbdFile)
			require.NoError(t, err)
			if size != 0 {
				assert.Equal(t, numBlocks*blockSize, size, "NBD device size")
				break loop
			}
		}
	}

	stopArg := spdk.StopNBDDiskArgs{NBDDevice: nbdDevice}
	err = spdk.StopNBDDisk(ctx, client, stopArg)
	require.NoError(t, err, "Stop NBD Disk with %+v", stopArg)
}

func TestSCSI(t *testing.T) {
	defer testlog.SetGlobal(t)()
	ctx := context.Background()
	defer testspdk.Finalize()
	client := connect(t)

	var err error

	checkControllers := func(t *testing.T, expected spdk.GetVHostControllersResponse) {
		controllers, err := spdk.GetVHostControllers(ctx, client)
		require.NoError(t, err, "GetVHostControllers")
		assert.Equal(t, expected, controllers)
	}

	controller := "my-scsi-vhost"
	constructArgs := spdk.ConstructVHostSCSIControllerArgs{
		Controller: controller,
	}
	err = spdk.ConstructVHostSCSIController(ctx, client, constructArgs)
	require.NoError(t, err, "Construct VHostSCSI controller with %v", constructArgs)
	defer spdk.RemoveVHostController(ctx, client, spdk.RemoveVHostControllerArgs{Controller: controller})

	expected := spdk.GetVHostControllersResponse{
		spdk.Controller{
			Controller: controller,
			CPUMask:    "0x1",
			BackendSpecific: spdk.BackendSpecificType{
				"scsi": spdk.SCSIControllerSpecific{},
			},
		},
	}
	checkControllers(t, expected)

	bdevArgs := spdk.ConstructMallocBDevArgs{ConstructBDevArgs: spdk.ConstructBDevArgs{NumBlocks: 2048, BlockSize: 512}}
	created, err := spdk.ConstructMallocBDev(ctx, client, bdevArgs)
	require.NoError(t, err, "Construct Malloc BDev with %v", bdevArgs)
	defer spdk.DeleteBDev(ctx, client, spdk.DeleteBDevArgs{Name: string(created)})
	created2, err := spdk.ConstructMallocBDev(ctx, client, bdevArgs)
	require.NoError(t, err, "Construct Malloc BDev with %v", bdevArgs)
	defer spdk.DeleteBDev(ctx, client, spdk.DeleteBDevArgs{Name: string(created2)})

	addLUN := spdk.AddVHostSCSILUNArgs{
		Controller: controller,
		BDevName:   string(created),
	}
	err = spdk.AddVHostSCSILUN(ctx, client, addLUN)
	require.NoError(t, err, "AddVHostSCSILUN %v", addLUN)
	expected[0].BackendSpecific["scsi"] = spdk.SCSIControllerSpecific{
		spdk.SCSIControllerTarget{
			TargetName: "Target 0",
			LUNs: []spdk.SCSIControllerLUN{
				spdk.SCSIControllerLUN{
					BDevName: string(created),
				},
			},
		},
	}
	checkControllers(t, expected)

	addLUN2 := spdk.AddVHostSCSILUNArgs{
		Controller:    controller,
		SCSITargetNum: 1,
		BDevName:      string(created2),
	}
	err = spdk.AddVHostSCSILUN(ctx, client, addLUN2)
	require.NoError(t, err, "AddVHostSCSILUN %v", addLUN2)
	expected[0].BackendSpecific["scsi"] = spdk.SCSIControllerSpecific{
		spdk.SCSIControllerTarget{
			TargetName: "Target 0",
			LUNs: []spdk.SCSIControllerLUN{
				spdk.SCSIControllerLUN{
					BDevName: string(created),
				},
			},
		},
		spdk.SCSIControllerTarget{
			TargetName: "Target 1",
			ID:         1,
			SCSIDevNum: 1,
			LUNs: []spdk.SCSIControllerLUN{
				spdk.SCSIControllerLUN{
					BDevName: string(created2),
				},
			},
		},
	}
	checkControllers(t, expected)

	controller2 := "my-scsi-vhost2"
	constructArgs2 := spdk.ConstructVHostSCSIControllerArgs{
		Controller: controller2,
	}
	err = spdk.ConstructVHostSCSIController(ctx, client, constructArgs2)
	require.NoError(t, err, "Construct VHostSCSI controller with %v", constructArgs2)
	defer spdk.RemoveVHostController(ctx, client, spdk.RemoveVHostControllerArgs{Controller: controller2})

	expected = append(expected,
		spdk.Controller{
			Controller: controller2,
			CPUMask:    "0x1",
			BackendSpecific: spdk.BackendSpecificType{
				"scsi": spdk.SCSIControllerSpecific{},
			},
		})
	checkControllers(t, expected)

	removeArgs := spdk.RemoveVHostSCSITargetArgs{
		Controller:    controller,
		SCSITargetNum: 0,
	}
	err = spdk.RemoveVHostSCSITarget(ctx, client, removeArgs)
	require.NoError(t, err, "RemoveVHostSCSITarget %v", removeArgs)
	expected[0].BackendSpecific["scsi"] = spdk.SCSIControllerSpecific{
		spdk.SCSIControllerTarget{
			TargetName: "Target 1",
			ID:         1,
			SCSIDevNum: 1,
			LUNs: []spdk.SCSIControllerLUN{
				spdk.SCSIControllerLUN{
					BDevName: string(created2),
				},
			},
		},
	}
	checkControllers(t, expected)

	// Cannot remove non-empty controller.
	err = spdk.RemoveVHostController(ctx, client, spdk.RemoveVHostControllerArgs{Controller: controller})
	require.Error(t, err, "Remove VHost controller %s", controller)
	checkControllers(t, expected)

	err = spdk.RemoveVHostController(ctx, client, spdk.RemoveVHostControllerArgs{Controller: controller2})
	require.NoError(t, err, "Remove VHost controller %s", controller2)
	expected = expected[0:1]
	checkControllers(t, expected)
}
