/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"context"
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/pkg/errors"

	"github.com/intel/oim/pkg/oim-common"
	"google.golang.org/grpc"

	csi0 "github.com/intel/oim/pkg/spec/csi/v0"
	"github.com/intel/oim/pkg/spec/oim/v0"
)

const (
	kib int64 = 1024
	mib int64 = kib * 1024
	gib int64 = mib * 1024
	tib int64 = gib * 1024

	maxStorageCapacity = tib // TODO: we don't really know the upper limit
)

// Driver is the public interface for managing the OIM CSI driver.
type Driver interface {
	Start(ctx context.Context) (*oimcommon.NonBlockingGRPCServer, error)
	Run(ctx context.Context) error
}

// oimDriver is the actual implementation based on CSI 1.0.
type oimDriver struct {
	driverName            string
	version               string
	csiVersion            string
	nodeID                string
	csiEndpoint           string
	remote                remoteSPDK
	local                 localSPDK
	emulatedCSIDriverName string

	backend OIMBackend

	cap []*csi.ControllerServiceCapability
	vc  []*csi.VolumeCapability_AccessMode
}

// oimDriver1 is the implementation based on the legacy CSI 1.0. It gets most of its
// implementation from the oimDriver.
type oimDriver03 struct {
	oimDriver

	cap []*csi0.ControllerServiceCapability
	vc  []*csi0.VolumeCapability_AccessMode
}

type cleanup func() error

// OIMBackend defines the actual implementation of several operations.
// It has two implementations:
// - OIM CSI driver directly controlling SPDK running on the same host (local.go)
// - OIM CSI driver controlling SPDK through OIM registry and controller (remote.go)
type OIMBackend interface {
	createVolume(ctx context.Context, volumeID string, requiredBytes int64) (int64, error)
	deleteVolume(ctx context.Context, volumeID string) error
	checkVolumeExists(ctx context.Context, volumeID string) error

	createDevice(ctx context.Context, volumeID string, request interface{}) (string, cleanup, error)
	deleteDevice(ctx context.Context, volumeID string) error
}

// EmulateCSI0Driver deals with parameters meant for some other CSI v0.3 driver.
type EmulateCSI0Driver struct {
	CSIDriverName                 string
	ControllerServiceCapabilities []csi0.ControllerServiceCapability_RPC_Type
	VolumeCapabilityAccessModes   []csi0.VolumeCapability_AccessMode_Mode
	MapVolumeParams               func(from *csi0.NodeStageVolumeRequest, to *oim.MapVolumeRequest) error
}

// EmulateCSIDriver deals with parameters meant for some other CSI v1.0 driver.
type EmulateCSIDriver struct {
	CSIDriverName                 string
	ControllerServiceCapabilities []csi.ControllerServiceCapability_RPC_Type
	VolumeCapabilityAccessModes   []csi.VolumeCapability_AccessMode_Mode
	MapVolumeParams               func(from *csi.NodeStageVolumeRequest, to *oim.MapVolumeRequest) error
}

var (
	supportedCSI0Drivers = make(map[string]*EmulateCSI0Driver)
	supportedCSIDrivers  = make(map[string]*EmulateCSIDriver)
)

// Option is the type-safe parameter for configuring New.
type Option func(*oimDriver) error

// WithDriverName overrides the default CSI driver name.
func WithDriverName(name string) Option {
	return func(od *oimDriver) error {
		od.driverName = name
		return nil
	}
}

// WithDriverVersion sets the version reported by the driver.
func WithDriverVersion(version string) Option {
	return func(od *oimDriver) error {
		od.version = version
		return nil
	}
}

const (
	csi10 = "1.0"
	csi03 = "0.3"
)

// WithCSIVersion sets the CSI version that is to be implemented by the driver.
func WithCSIVersion(version string) Option {
	return func(od *oimDriver) error {
		od.csiVersion = version
		return nil
	}
}

// WithNodeID sets the node ID reported by the driver.
func WithNodeID(id string) Option {
	return func(od *oimDriver) error {
		od.nodeID = id
		return nil
	}
}

// WithCSIEndpoint determines what the driver listens on.
// Uses the same unix:// or tcp:// prefix as other CSI
// drivers to determine the network.
func WithCSIEndpoint(endpoint string) Option {
	return func(od *oimDriver) error {
		od.csiEndpoint = endpoint
		return nil
	}
}

// WithVHostEndpoint sets the net.Dial string for
// the SPDK RPC communication.
func WithVHostEndpoint(endpoint string) Option {
	return func(od *oimDriver) error {
		od.local.vhostEndpoint = endpoint
		return nil
	}
}

// WithOIMRegistryAddress sets the gRPC dial string for
// contacting the OIM registry.
func WithOIMRegistryAddress(address string) Option {
	return func(od *oimDriver) error {
		od.remote.oimRegistryAddress = address
		return nil
	}
}

// WithRegistryCreds sets the TLS key and CA for
// connections to the OIM registry.
func WithRegistryCreds(ca, key string) Option {
	return func(od *oimDriver) error {
		od.remote.registryCA = ca
		od.remote.registryKey = key
		return nil
	}
}

// WithOIMControllerID sets the ID assigned to the
// controller that is responsible for the host.
func WithOIMControllerID(id string) Option {
	return func(od *oimDriver) error {
		od.remote.oimControllerID = id
		return nil
	}
}

// WithEmulation switches between different personalities:
// in this mode, the OIM CSI driver handles arguments for
// some other, "emulated" CSI driver and redirects local
// node operations to the OIM controller.
func WithEmulation(csiDriverName string) Option {
	return func(od *oimDriver) error {
		od.emulatedCSIDriverName = csiDriverName
		return nil
	}
}

// New constructs a new OIM driver instance.
func New(options ...Option) (Driver, error) {
	od := oimDriver03{
		oimDriver: oimDriver{
			driverName:  "oim-driver",
			csiVersion:  csi10,
			version:     "unknown",
			nodeID:      "unset-node-id",
			csiEndpoint: "unix:///var/run/oim-driver.socket",
		},
	}
	for _, op := range options {
		err := op(&od.oimDriver)
		if err != nil {
			return nil, err
		}
	}
	if od.local.vhostEndpoint != "" && od.remote.oimRegistryAddress != "" {
		return nil, errors.New("SPDK and OIM registry usage are mutually exclusive")
	}
	if od.local.vhostEndpoint == "" && od.remote.oimRegistryAddress == "" {
		return nil, errors.New("Either SPDK or OIM registry must be selected")
	}
	if od.remote.oimRegistryAddress != "" && (od.remote.oimControllerID == "" ||
		od.remote.registryCA == "" ||
		od.remote.registryKey == "") {
		return nil, errors.New("Cannot use a OIM registry without a controller ID, CA file and key file")
	}
	// malloc capabilities
	switch od.csiVersion {
	case csi03:
		od.setControllerServiceCapabilities([]csi0.ControllerServiceCapability_RPC_Type{csi0.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME})
		od.setVolumeCapabilityAccessModes([]csi0.VolumeCapability_AccessMode_Mode{csi0.VolumeCapability_AccessMode_SINGLE_NODE_WRITER})
	case csi10:
		od.oimDriver.setControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME})
		od.oimDriver.setVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER})
	default:
		return nil, errors.Errorf("running as CSI version %q not supported", od.csiVersion)
	}
	if od.local.vhostEndpoint != "" {
		if od.emulatedCSIDriverName != "" {
			return nil, errors.Errorf("emulating CSI driver %q not currently implemented when using SPDK directly", od.emulatedCSIDriverName)
		}
		od.backend = &od.local
	} else {
		if od.emulatedCSIDriverName != "" {
			switch od.csiVersion {
			case csi03:
				emulate := supportedCSI0Drivers[od.emulatedCSIDriverName]
				if emulate == nil {
					return nil, fmt.Errorf("cannot emulate CSI 0.3 driver %q", od.emulatedCSIDriverName)
				}
				od.remote.mapVolumeParams = func(request interface{}, to *oim.MapVolumeRequest) error {
					from := request.(*csi0.NodeStageVolumeRequest)
					return emulate.MapVolumeParams(from, to)
				}
				od.setControllerServiceCapabilities(emulate.ControllerServiceCapabilities)
				od.setVolumeCapabilityAccessModes(emulate.VolumeCapabilityAccessModes)
			case csi10:
				emulate := supportedCSIDrivers[od.emulatedCSIDriverName]
				if emulate == nil {
					return nil, fmt.Errorf("cannot emulate CSI 1.0 driver %q", od.emulatedCSIDriverName)
				}
				od.remote.mapVolumeParams = func(request interface{}, to *oim.MapVolumeRequest) error {
					from := request.(*csi.NodeStageVolumeRequest)
					return emulate.MapVolumeParams(from, to)
				}
				od.oimDriver.setControllerServiceCapabilities(emulate.ControllerServiceCapabilities)
				od.oimDriver.setVolumeCapabilityAccessModes(emulate.VolumeCapabilityAccessModes)
			}
		}
		od.backend = &od.remote
	}
	return &od, nil
}

func (od *oimDriver03) Start(ctx context.Context) (*oimcommon.NonBlockingGRPCServer, error) {
	s := oimcommon.NonBlockingGRPCServer{
		Endpoint: od.csiEndpoint,
	}
	s.Start(ctx, func(s *grpc.Server) {
		switch od.csiVersion {
		case csi03:
			csi0.RegisterIdentityServer(s, od)
			csi0.RegisterNodeServer(s, od)
			csi0.RegisterControllerServer(s, od)
		case csi10:
			csi.RegisterIdentityServer(s, &od.oimDriver)
			csi.RegisterNodeServer(s, &od.oimDriver)
			csi.RegisterControllerServer(s, &od.oimDriver)
		}
	})
	return &s, nil
}

func (od *oimDriver03) Run(ctx context.Context) error {
	s, err := od.Start(ctx)
	if err != nil {
		return err
	}
	s.Wait(ctx)
	return nil
}
