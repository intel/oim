/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"errors"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
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

var (
	//	oimDriver     *oim
	vendorVersion = "0.2.0"
)

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

func (od *oimDriver) Start(driverName, nodeID, endpoint string) (NonBlockingGRPCServer, error) {
	// Initialize default library driver
	od.driver = NewCSIDriver(driverName, vendorVersion, nodeID)
	if od.driver == nil {
		return nil, errors.New("Failed to initialize CSI Driver.")
	}
	od.driver.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME})
	od.driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER})

	// Create GRPC servers
	od.ids = NewIdentityServer(od.driver)
	od.ns = NewNodeServer(od.driver)
	od.cs = NewControllerServer(od.driver)

	s := NewNonBlockingGRPCServer()
	s.Start(endpoint, od.ids, od.cs, od.ns)
	return s, nil
}

func (od *oimDriver) Run(driverName, nodeID, endpoint string) error {
	s, err := od.Start(driverName, nodeID, endpoint)
	if err != nil {
		return err
	}
	s.Wait()
	return nil
}
