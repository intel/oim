/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package spdk

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func connect(t *testing.T) *Client {
	path := "/var/tmp/spdk.sock"
	client, err := New(path)
	require.NoError(t, err, "Failed to contact SPDK at %s", path)
	require.NotNil(t, client, "No client")
	return client
}

func TestGetBDevs(t *testing.T) {
	client := connect(t)
	defer client.Close()
	args := []*GetBDevsArgs{
		nil,
		&GetBDevsArgs{},
	}
	for _, arg := range args {
		response, err := GetBDevs(context.Background(), client, arg)
		assert.NoError(t, err, "Failed to list bdevs with %+v: %s", arg)
		assert.Empty(t, response, "Unexpected non-empty bdev list")
	}
}

func TestMallocBDev(t *testing.T) {
	ctx := context.Background()
	client := connect(t)
	defer client.Close()

	var created ConstructBDevResponse
	cleanup := func(when string) {
		t.Logf("Cleaning up at %s: %+v", when, created)
		for _, bdev := range created {
			err := DeleteBDev(ctx, client, &DeleteBDevArgs{Name: bdev})
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
		created, err = ConstructMallocBDev(ctx, client, &arg)
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
		bdevs, err := GetBDevs(ctx, client, &GetBDevsArgs{Name: name})
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
