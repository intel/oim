/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package spdk

import (
	"bytes"
	"context"
	"testing"

	"github.com/mafredri/cdp/rpcc"
)

func TestEncode(t *testing.T) {
	buffer := bytes.NewBufferString("")
	c := newJSON2Codec(buffer)
	r := rpcc.Request{ID: 1, Method: "foo"}
	err := c.WriteRequest(&r)
	if err != nil {
		t.Fatalf("Encoding %v failed: %v", r, err)
	}
	encoded := string(buffer.Bytes())
	expected := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"foo\"}\n"
	if encoded != expected {
		t.Errorf("Unexpected encoding: expected %q, got %q", expected, encoded)
	}
}

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
