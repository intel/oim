/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"context"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNodeGetId(t *testing.T) {
	d := NewFakeDriver()

	ns := NewDefaultNodeServer(d)

	// Test valid request
	req := csi.NodeGetIdRequest{}
	resp, err := ns.NodeGetId(context.Background(), &req)
	assert.NoError(t, err)
	assert.Equal(t, resp.GetNodeId(), fakeNodeID)
}

func TestNodeGetCapabilities(t *testing.T) {
	d := NewFakeDriver()

	ns := NewDefaultNodeServer(d)

	// Test valid request
	req := csi.NodeGetCapabilitiesRequest{}
	_, err := ns.NodeGetCapabilities(context.Background(), &req)
	assert.NoError(t, err)
}

func TestNodePublishVolume(t *testing.T) {
	d := NewFakeDriver()

	ns := NewDefaultNodeServer(d)

	// Test invalid request
	req := csi.NodePublishVolumeRequest{}
	_, err := ns.NodePublishVolume(context.Background(), &req)
	s, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, s.Code(), codes.Unimplemented)
}

func TestNodeUnpublishVolume(t *testing.T) {
	d := NewFakeDriver()

	ns := NewDefaultNodeServer(d)

	// Test invalid request
	req := csi.NodeUnpublishVolumeRequest{}
	_, err := ns.NodeUnpublishVolume(context.Background(), &req)
	s, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, s.Code(), codes.Unimplemented)
}
