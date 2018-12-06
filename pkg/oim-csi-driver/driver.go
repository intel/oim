/*
Copyright 2017 The Kubernetes Authors.
Copyright (C) 2018 Intel Corporation

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"github.com/container-storage-interface/spec/lib/go/csi/v0"
)

func (od *oimDriver) setControllerServiceCapabilities(cl []csi.ControllerServiceCapability_RPC_Type) {
	var csc []*csi.ControllerServiceCapability

	for _, c := range cl {
		csc = append(csc,
			&csi.ControllerServiceCapability{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: c,
					},
				},
			})
	}

	od.cap = csc
}

func (od *oimDriver) setVolumeCapabilityAccessModes(vc []csi.VolumeCapability_AccessMode_Mode) {
	var vca []*csi.VolumeCapability_AccessMode
	for _, c := range vc {
		vca = append(vca,
			&csi.VolumeCapability_AccessMode{
				Mode: c,
			})
	}
	od.vc = vca
}
