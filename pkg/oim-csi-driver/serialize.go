/*
Copyright (C) 2018 Intel Corporation

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"k8s.io/utils/keymutex"
)

var (
	// Volume names are the keys.
	volumeNameMutex = keymutex.NewHashed(-1)
)
