/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package spdk

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intel/oim/pkg/oim-common"
)

func connect(t *testing.T) *Client {
	path := os.Getenv("TEST_SPDK_VHOST_SOCKET")
	if path == "" {
		t.Skip("No SPDK vhost, TEST_SPDK_VHOST_SOCKET is empty.")
	}
	client, err := New(path)
	require.NoError(t, err, "Failed to contact SPDK at %s", path)
	require.NotNil(t, client, "No client")
	return client
}

func TestGetBDevs(t *testing.T) {
	client := connect(t)
	defer client.Close()
	response, err := GetBDevs(context.Background(), client, GetBDevsArgs{})
	assert.NoError(t, err, "Failed to list bdevs: %s", err)
	assert.Empty(t, response, "Unexpected non-empty bdev list")
}

func TestError(t *testing.T) {
	client := connect(t)
	defer client.Close()

	// It would be nice to get a well-documented error code here,
	// but currently we don't (https://github.com/spdk/spdk/issues/319).
	_, err := GetBDevs(context.Background(), client, GetBDevsArgs{Name: "no-such-bdev"})
	require.Error(t, err, "Should have failed to find no-such-bdev")
	require.True(t, IsJSONError(err, ERROR_INVALID_PARAMS), "IsJSONError(%+v, ERROR_INVALID_PARAMS)", err)
}

func TestMallocBDev(t *testing.T) {
	ctx := context.Background()
	client := connect(t)
	defer client.Close()

	var created ConstructBDevResponse
	cleanup := func(when string) {
		t.Logf("Cleaning up at %s: %+v", when, created)
		for _, bdev := range created {
			err := DeleteBDev(ctx, client, DeleteBDevArgs{Name: bdev})
			require.NoError(t, err, "Failed to delete bdev %s: %s", bdev)
		}
		created = ConstructBDevResponse{}
	}
	defer cleanup("deferred cleanup")

	// 1MB seems to be the minimum size?
	for i, arg := range []ConstructMallocBDevArgs{
		ConstructMallocBDevArgs{ConstructBDevArgs{NumBlocks: 2048, BlockSize: 512}},
		ConstructMallocBDevArgs{ConstructBDevArgs{NumBlocks: 4096, BlockSize: 512, Name: "MyMallocBdev", UUID: "11111111-2222-3333-4444-555555555555"}},
	} {
		cleanup(fmt.Sprintf("bdev %d", i))
		// Can't use := here, it would shadow "created"!
		var err error
		created, err = ConstructMallocBDev(ctx, client, arg)
		if !assert.NoError(t, err, "Failed to create %+v", arg) {
			continue
		}
		t.Logf("Created %+v", created)
		if created == nil || len(created) != 1 {
			t.Errorf("Should have received exactly one Malloc* bdev name: %+v", created)
			continue
		}
		name := created[0]
		if arg.Name != "" {
			assert.Equal(t, arg.Name, name, "choosen name")
		}
		bdevs, err := GetBDevs(ctx, client, GetBDevsArgs{Name: name})
		t.Logf("bdev %s attributes: %+v", name, bdevs)
		if !assert.NoError(t, err, "Failed to retrieve bdev %s attributes", name) {
			continue
		}
		if len(bdevs) != 1 {
			t.Errorf("Should have received exactly one bdev")
			continue
		}
		bdev := bdevs[0]
		expected := BDev{
			Name:        name,
			ProductName: "Malloc disk",
			BlockSize:   arg.BlockSize,
			NumBlocks:   arg.NumBlocks,
			UUID:        arg.UUID,
			SupportedIOTypes: SupportedIOTypes{
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
	ctx := context.Background()
	client := connect(t)
	defer client.Close()

	name := "my_malloc_bdev"
	numBlocks := int64(2048)
	blockSize := int64(512)
	createArg := ConstructMallocBDevArgs{ConstructBDevArgs{NumBlocks: numBlocks, BlockSize: blockSize, Name: name}}
	// TODO: this does not get called when the test fails?
	defer func() {
		DeleteBDev(ctx, client, DeleteBDevArgs{Name: name})
	}()
	_, err := ConstructMallocBDev(ctx, client, createArg)
	require.NoError(t, err, "Failed to create %+v", createArg)

	nbd, err := GetNBDDisks(ctx, client)
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

	startArg := StartNBDDiskArgs{BDevName: name, NBDDevice: nbdDevice}
	err = StartNBDDisk(ctx, client, startArg)
	require.NoError(t, err, "Start NBD Disk with %+v", startArg)

	nbd, err = GetNBDDisks(ctx, client)
	assert.NoError(t, err, "get initial list of disks")
	assert.Equal(t, nbd, GetNBDDisksResponse{startArg}, "should have one NBD device running")

	size, err := oimcommon.GetBlkSize64(nbdFile)
	require.NoError(t, err)
	assert.Equal(t, numBlocks*blockSize, size, "NBD device size")

	stopArg := StopNBDDiskArgs{NBDDevice: nbdDevice}
	err = StopNBDDisk(ctx, client, stopArg)
	require.NoError(t, err, "Stop NBD Disk with %+v", stopArg)
}

func TestSCSI(t *testing.T) {
	ctx := context.Background()
	client := connect(t)
	defer client.Close()

	var err error

	checkControllers := func(t *testing.T, expected GetVHostControllersResponse) {
		controllers, err := GetVHostControllers(ctx, client)
		require.NoError(t, err, "GetVHostControllers")
		assert.Equal(t, expected, controllers)
	}

	controller := "my-scsi-vhost"
	constructArgs := ConstructVHostSCSIControllerArgs{
		Controller: controller,
	}
	err = ConstructVHostSCSIController(ctx, client, constructArgs)
	require.NoError(t, err, "Construct VHostSCSI controller with %v", constructArgs)
	defer RemoveVHostController(ctx, client, RemoveVHostControllerArgs{Controller: controller})

	expected := GetVHostControllersResponse{
		Controller{
			Controller: controller,
			CPUMask:    "0x1",
			BackendSpecific: BackendSpecificType{
				"scsi": SCSIControllerSpecific{},
			},
		},
	}
	checkControllers(t, expected)

	bdevArgs := ConstructMallocBDevArgs{ConstructBDevArgs{NumBlocks: 2048, BlockSize: 512}}
	created, err := ConstructMallocBDev(ctx, client, bdevArgs)
	require.NoError(t, err, "Construct Malloc BDev with %v", bdevArgs)
	defer DeleteBDev(ctx, client, DeleteBDevArgs{Name: created[0]})
	created2, err := ConstructMallocBDev(ctx, client, bdevArgs)
	require.NoError(t, err, "Construct Malloc BDev with %v", bdevArgs)
	defer DeleteBDev(ctx, client, DeleteBDevArgs{Name: created2[0]})

	addLUN := AddVHostSCSILUNArgs{
		Controller: controller,
		BDevName:   created[0],
	}
	err = AddVHostSCSILUN(ctx, client, addLUN)
	require.NoError(t, err, "AddVHostSCSILUN %v", addLUN)
	expected[0].BackendSpecific["scsi"] = SCSIControllerSpecific{
		SCSIControllerTarget{
			TargetName: "Target 0",
			LUNs: []SCSIControllerLUN{
				SCSIControllerLUN{
					BDevName: created[0],
				},
			},
		},
	}
	checkControllers(t, expected)

	addLUN2 := AddVHostSCSILUNArgs{
		Controller:    controller,
		SCSITargetNum: 1,
		BDevName:      created2[0],
	}
	err = AddVHostSCSILUN(ctx, client, addLUN2)
	require.NoError(t, err, "AddVHostSCSILUN %v", addLUN2)
	expected[0].BackendSpecific["scsi"] = SCSIControllerSpecific{
		SCSIControllerTarget{
			TargetName: "Target 0",
			LUNs: []SCSIControllerLUN{
				SCSIControllerLUN{
					BDevName: created[0],
				},
			},
		},
		SCSIControllerTarget{
			TargetName: "Target 1",
			ID:         1,
			SCSIDevNum: 1,
			LUNs: []SCSIControllerLUN{
				SCSIControllerLUN{
					BDevName: created2[0],
				},
			},
		},
	}
	checkControllers(t, expected)

	controller2 := "my-scsi-vhost2"
	constructArgs2 := ConstructVHostSCSIControllerArgs{
		Controller: controller2,
	}
	err = ConstructVHostSCSIController(ctx, client, constructArgs2)
	require.NoError(t, err, "Construct VHostSCSI controller with %v", constructArgs2)
	defer RemoveVHostController(ctx, client, RemoveVHostControllerArgs{Controller: controller2})

	expected = append(expected,
		Controller{
			Controller: controller2,
			CPUMask:    "0x1",
			BackendSpecific: BackendSpecificType{
				"scsi": SCSIControllerSpecific{},
			},
		})
	checkControllers(t, expected)

	removeArgs := RemoveVHostSCSITargetArgs{
		Controller:    controller,
		SCSITargetNum: 0,
	}
	err = RemoveVHostSCSITarget(ctx, client, removeArgs)
	require.NoError(t, err, "RemoveVHostSCSITarget %v", removeArgs)
	expected[0].BackendSpecific["scsi"] = SCSIControllerSpecific{
		SCSIControllerTarget{
			TargetName: "Target 1",
			ID:         1,
			SCSIDevNum: 1,
			LUNs: []SCSIControllerLUN{
				SCSIControllerLUN{
					BDevName: created2[0],
				},
			},
		},
	}
	checkControllers(t, expected)

	// Cannot remove non-empty controller.
	err = RemoveVHostController(ctx, client, RemoveVHostControllerArgs{Controller: controller})
	require.Error(t, err, "Remove VHost controller %s", controller)
	checkControllers(t, expected)

	err = RemoveVHostController(ctx, client, RemoveVHostControllerArgs{Controller: controller2})
	require.NoError(t, err, "Remove VHost controller %s", controller2)
	expected = expected[0:1]
	checkControllers(t, expected)
}
