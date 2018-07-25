/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"os"
)

// GetBlkSize64 returns the size of a block device, referred to with
// an open read/write or read-only file handle.
func GetBlkSize64(file *os.File) (int64, error) {
	// The "right" way
	// to do this is via the BLKGETSIZE64 ioctl, but using that is
	// complicated in Go, so what's done instead is to seek to the end
	// of the file.

	// Always return to the current offset.
	curr, err := file.Seek(0, 1)
	if err != nil {
		return 0, err
	}
	defer file.Seek(curr, 0)

	size64, err := file.Seek(0, 2)
	return size64, err
}
