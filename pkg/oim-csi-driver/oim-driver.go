/*
Copyright 2017 The Kubernetes Authors.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/golang/glog"
)

const (
	kib    int64 = 1024
	mib    int64 = kib * 1024
	gib    int64 = mib * 1024
	gib100 int64 = gib * 100
	tib    int64 = gib * 1024
	tib100 int64 = tib * 100
)

type oimDriver struct {
	driver *CSIDriver

	ids *identityServer
	ns  *nodeServer
	cs  *controllerServer

	cap   []*csi.VolumeCapability_AccessMode
	cscap []*csi.ControllerServiceCapability
}

type oimVolume struct {
	VolName string `json:"volName"`
	VolID   string `json:"volID"`
	VolSize int64  `json:"volSize"`
	VolPath string `json:"volPath"`
}

var oimVolumes map[string]oimVolume

var (
	//	oimDriver     *oim
	vendorVersion = "0.2.0"
)

func init() {
	oimVolumes = map[string]oimVolume{}
}

func GetOIMDriver() *oimDriver {
	return &oimDriver{}
}

func NewIdentityServer(d *CSIDriver) *identityServer {
	return &identityServer{
		DefaultIdentityServer: NewDefaultIdentityServer(d),
	}
}

func NewControllerServer(d *CSIDriver) *controllerServer {
	return &controllerServer{
		DefaultControllerServer: NewDefaultControllerServer(d),
	}
}

func NewNodeServer(d *CSIDriver) *nodeServer {
	return &nodeServer{
		DefaultNodeServer: NewDefaultNodeServer(d),
	}
}

func (od *oimDriver) Run(driverName, nodeID, endpoint string) {
	glog.Infof("Driver: %v ", driverName)

	// Initialize default library driver
	od.driver = NewCSIDriver(driverName, vendorVersion, nodeID)
	if od.driver == nil {
		glog.Fatalln("Failed to initialize CSI Driver.")
	}
	od.driver.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME})
	od.driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER})

	// Create GRPC servers
	od.ids = NewIdentityServer(od.driver)
	od.ns = NewNodeServer(od.driver)
	od.cs = NewControllerServer(od.driver)

	s := NewNonBlockingGRPCServer()
	s.Start(endpoint, od.ids, od.cs, od.ns)
	s.Wait()
}

func getVolumeByID(volumeID string) (oimVolume, error) {
	if oimVol, ok := oimVolumes[volumeID]; ok {
		return oimVol, nil
	}
	return oimVolume{}, fmt.Errorf("volume id %s does not exit in the volumes list", volumeID)
}

func getVolumeByName(volName string) (oimVolume, error) {
	for _, oimVol := range oimVolumes {
		if oimVol.VolName == volName {
			return oimVol, nil
		}
	}
	return oimVolume{}, fmt.Errorf("volume name %s does not exit in the volumes list", volName)
}
