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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intel/oim/pkg/log/testlog"
)

func TestFindDev(t *testing.T) {
	defer testlog.SetGlobal(t)()
	ctx := context.Background()

	var err error
	var dev string
	var major, minor int

	tmp, err := ioutil.TempDir("", "find-dev")
	require.NoError(t, err)
	defer os.RemoveAll(tmp)

	// Nothing in empty dir.
	dev, major, minor, err = findDev(ctx, tmp, "foo", "0:0")
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
	dev, major, minor, err = findDev(ctx, tmp, "foo", "0:0")
	assert.NoError(t, err)
	assert.Equal(t, "", dev)

	// Closer, but not quite.
	dev, major, minor, err = findDev(ctx, tmp, "/devices/pci0000:00/0000:00:17.0/", "5:0")
	assert.NoError(t, err)
	assert.Equal(t, "", dev)

	// Find sda.
	dev, major, minor, err = findDev(ctx, tmp, "/devices/pci0000:00/0000:00:17.0/", "0:0")
	assert.NoError(t, err)
	assert.Equal(t, "sda", dev)
	assert.Equal(t, major, 8)
	assert.Equal(t, minor, 0)

	// Without SCSI.
	dev, major, minor, err = findDev(ctx, tmp, "/devices/pci0000:00/0000:00:18.0/", "")
	assert.NoError(t, err)
	assert.Equal(t, "", dev)

	// Find sda.
	dev, major, minor, err = findDev(ctx, tmp, "/devices/pci0000:00/0000:00:17.0/", "")
	assert.NoError(t, err)
	assert.Equal(t, "sda", dev)

	// No deadline.
	dev, major, minor, err = waitForDevice(ctx, tmp, "/devices/pci0000:00/0000:00:17.0/", "0:0")
	assert.NoError(t, err)
	assert.Equal(t, "sda", dev)
	assert.Equal(t, major, 8)
	assert.Equal(t, minor, 0)

	// Timeout aborts wait.
	timeout, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	dev, major, minor, err = waitForDevice(timeout, tmp, "/devices/pci0000:00/0000:00:17.0/", "1:0")
	assert.Error(t, err)
	assert.Equal(t, "rpc error: code = DeadlineExceeded desc = timed out waiting for device '/devices/pci0000:00/0000:00:17.0/', SCSI unit '1:0'", err.Error())

	// Create the expected entry in two seconds, wait at most five.
	timeout2, cancel := context.WithTimeout(ctx, 5*time.Second)
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
	dev, major, minor, err = findDev(ctx, tmp, "/devices/pci0000:00/0000:00:17.0/", "2:0")
	if assert.Error(t, err) {
		assert.Equal(t, fmt.Sprintf("Unexpected entry in %s, not a major:minor symlink: a:b", tmp), err.Error())
	}
}
