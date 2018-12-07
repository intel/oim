/*
Copyright (C) 2018 Intel Corporation

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"k8s.io/kubernetes/pkg/util/keymutex" // TODO: move to k8s.io/utils (https://github.com/kubernetes/utils/issues/62)
)

var (
	// Volume names are the keys.
	volumeNameMutex = keymutex.NewHashed(-1)
)
