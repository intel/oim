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
	driverName    string
	nodeID        string
	csiEndpoint   string
	vhostEndpoint string

	driver *CSIDriver

	ids   *identityServer
	ns    *nodeServer
	cs    *controllerServer
	vhost string

	cap   []*csi.VolumeCapability_AccessMode
	cscap []*csi.ControllerServiceCapability
}

var (
	//	oimDriver     *oim
	vendorVersion = "0.2.0"
)

type DriverOption func(*oimDriver) error

func OptionDriverName(name string) DriverOption {
	return func(od *oimDriver) error {
		od.driverName = name
		return nil
	}
}

func OptionNodeID(id string) DriverOption {
	return func(od *oimDriver) error {
		od.nodeID = id
		return nil
	}
}

func OptionCSIEndpoint(endpoint string) DriverOption {
	return func(od *oimDriver) error {
		od.csiEndpoint = endpoint
		return nil
	}
}

func OptionVHostEndpoint(endpoint string) DriverOption {
	return func(od *oimDriver) error {
		od.vhostEndpoint = endpoint
		return nil
	}
}

func GetOIMDriver(options ...DriverOption) (*oimDriver, error) {
	od := oimDriver{
		driverName:  "oim-driver",
		nodeID:      "unset-node-id",
		csiEndpoint: "unix:///var/run/oim-driver.socket",
	}
	for _, op := range options {
		err := op(&od)
		if err != nil {
			return nil, err
		}
	}
	return &od, nil
}

func NewIdentityServer(od *oimDriver) *identityServer {
	return &identityServer{
		DefaultIdentityServer: NewDefaultIdentityServer(od.driver),
	}
}

func NewControllerServer(od *oimDriver) *controllerServer {
	return &controllerServer{
		DefaultControllerServer: NewDefaultControllerServer(od.driver),
		od: od,
	}
}

func NewNodeServer(od *oimDriver) *nodeServer {
	return &nodeServer{
		DefaultNodeServer: NewDefaultNodeServer(od.driver),
		od:                od,
	}
}

// TODO: concurrency protection
//
// By default, each gRPC call will execute in its own goroutine. That means
// that if an operation takes a long time and the sidecar decides to re-issue
// the call, we end up doing the same thing in parallel.
//
// We need to decide between a) serializing all calls or b) serializing
// only those calls related to the same item (bdev?).

func (od *oimDriver) Start() (NonBlockingGRPCServer, error) {
	// Initialize default library driver
	od.driver = NewCSIDriver(od.driverName, vendorVersion, od.nodeID)
	if od.driver == nil {
		return nil, errors.New("Failed to initialize CSI Driver.")
	}
	od.driver.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME})
	od.driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER})

	// Create GRPC servers
	od.ids = NewIdentityServer(od)
	od.ns = NewNodeServer(od)
	od.cs = NewControllerServer(od)

	s := NewNonBlockingGRPCServer()
	s.Start(od.csiEndpoint, od.ids, od.cs, od.ns)
	return s, nil
}

func (od *oimDriver) Run() error {
	s, err := od.Start()
	if err != nil {
		return err
	}
	s.Wait()
	return nil
}
