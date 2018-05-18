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
)

func TestGetPluginInfo(t *testing.T) {
	d := NewFakeDriver()

	ids := NewDefaultIdentityServer(d)

	req := csi.GetPluginInfoRequest{}
	resp, err := ids.GetPluginInfo(context.Background(), &req)
	assert.NoError(t, err)
	assert.Equal(t, resp.GetName(), fakeDriverName)
	assert.Equal(t, resp.GetVendorVersion(), vendorVersion)
}
