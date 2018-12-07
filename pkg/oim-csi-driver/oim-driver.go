/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"context"
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/pkg/errors"

	"github.com/intel/oim/pkg/oim-common"
	"google.golang.org/grpc"

	"github.com/intel/oim/pkg/spec/oim/v0"
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
	driverName         string
	version            string
	nodeID             string
	csiEndpoint        string
	vhostEndpoint      string
	oimRegistryAddress string
	registryCA         string
	registryKey        string
	oimControllerID    string
	emulate            *EmulateCSIDriver

	cap []*csi.ControllerServiceCapability
	vc  []*csi.VolumeCapability_AccessMode

	vhost string
}

type EmulateCSIDriver struct {
	CSIDriverName                 string
	ControllerServiceCapabilities []csi.ControllerServiceCapability_RPC_Type
	VolumeCapabilityAccessModes   []csi.VolumeCapability_AccessMode_Mode
	MapVolumeParams               func(from *csi.NodePublishVolumeRequest, to *oim.MapVolumeRequest) error
}

var (
	supportedCSIDrivers = make(map[string]*EmulateCSIDriver)
)

type Option func(*oimDriver) error

func WithDriverName(name string) Option {
	return func(od *oimDriver) error {
		od.driverName = name
		return nil
	}
}

func WithDriverVersion(version string) Option {
	return func(od *oimDriver) error {
		od.version = version
		return nil
	}
}

func WithNodeID(id string) Option {
	return func(od *oimDriver) error {
		od.nodeID = id
		return nil
	}
}

func WithCSIEndpoint(endpoint string) Option {
	return func(od *oimDriver) error {
		od.csiEndpoint = endpoint
		return nil
	}
}

func WithVHostEndpoint(endpoint string) Option {
	return func(od *oimDriver) error {
		od.vhostEndpoint = endpoint
		return nil
	}
}

func WithOIMRegistryAddress(address string) Option {
	return func(od *oimDriver) error {
		od.oimRegistryAddress = address
		return nil
	}
}

func WithRegistryCreds(ca, key string) Option {
	return func(od *oimDriver) error {
		od.registryCA = ca
		od.registryKey = key
		return nil
	}
}

func WithOIMControllerID(id string) Option {
	return func(od *oimDriver) error {
		od.oimControllerID = id
		return nil
	}
}

func WithEmulation(csiDriverName string) Option {
	return func(od *oimDriver) error {
		if csiDriverName == "" {
			od.emulate = nil
			return nil
		}
		emulate := supportedCSIDrivers[csiDriverName]
		if emulate == nil {
			return fmt.Errorf("cannot emulate CSI driver %q", csiDriverName)
		}
		od.emulate = emulate
		return nil
	}
}

func New(options ...Option) (*oimDriver, error) {
	od := oimDriver{
		driverName:  "oim-driver",
		version:     "unknown",
		nodeID:      "unset-node-id",
		csiEndpoint: "unix:///var/run/oim-driver.socket",
	}
	for _, op := range options {
		err := op(&od)
		if err != nil {
			return nil, err
		}
	}
	if od.vhostEndpoint != "" && od.oimRegistryAddress != "" {
		return nil, errors.New("SPDK and OIM registry usage are mutually exclusive")
	}
	if od.vhostEndpoint == "" && od.oimRegistryAddress == "" {
		return nil, errors.New("Either SPDK or OIM registry must be selected")
	}
	if od.oimRegistryAddress != "" && (od.oimControllerID == "" ||
		od.registryCA == "" ||
		od.registryKey == "") {
		return nil, errors.New("Cannot use a OIM registry without a controller ID, CA file and key file")
	}
	return &od, nil
}

func (od *oimDriver) Start(ctx context.Context) (*oimcommon.NonBlockingGRPCServer, error) {
	// Determine capabilities.
	if od.emulate != nil {
		od.setControllerServiceCapabilities(od.emulate.ControllerServiceCapabilities)
		od.setVolumeCapabilityAccessModes(od.emulate.VolumeCapabilityAccessModes)
	} else {
		// malloc fallback
		od.setControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME})
		od.setVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER})
	}

	s := oimcommon.NonBlockingGRPCServer{
		Endpoint: od.csiEndpoint,
	}
	s.Start(ctx, func(s *grpc.Server) {
		csi.RegisterIdentityServer(s, od)
		csi.RegisterNodeServer(s, od)
		csi.RegisterControllerServer(s, od)
	})
	return &s, nil
}

func (od *oimDriver) Run(ctx context.Context) error {
	s, err := od.Start(ctx)
	if err != nil {
		return err
	}
	s.Wait(ctx)
	return nil
}

func (od *oimDriver) DialRegistry(ctx context.Context) (*grpc.ClientConn, error) {
	// Intentionally loaded anew for each connection attempt.
	// File content can change over time.
	transportCreds, err := oimcommon.LoadTLS(od.registryCA, od.registryKey, "component.registry")
	if err != nil {
		return nil, errors.Wrap(err, "load TLS certs")
	}
	opts := oimcommon.ChooseDialOpts(od.oimRegistryAddress, grpc.WithTransportCredentials(transportCreds))
	conn, err := grpc.Dial(od.oimRegistryAddress, opts...)
	if err != nil {
		return nil, errors.Wrapf(err, "connect to OIM registry at %s", od.oimRegistryAddress)
	}
	return conn, nil
}
