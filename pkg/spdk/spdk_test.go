/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package spdk

import (
	"context"
	"testing"
)

func TestGetBDevs(t *testing.T) {
	client, err := New("/var/tmp/spdk.sock")
	if err != nil {
		t.Fatalf("Failed to contact SPDK: %s", err)
	}
	args := []*GetBDevsArgs{
		nil,
		&GetBDevsArgs{},
	}
	for _, arg := range args {
		response, err := GetBDevs(context.Background(), client, arg)
		if err != nil {
			t.Fatalf("Failed to list bdevs with %+v: %s", arg, err)
		}
		if len(response) > 0 {
			t.Errorf("Unexpected non-empty bdev list: %+v", response)
		}
		}
	}
}
