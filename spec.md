# Offline Infrastructure Manager (OIM)

Open Infrastructure Manager (OIM) is an open source project which
simplifies the integration of storage and network acceleration into
cloud environments like Kubernetes, Mesos and OpenStack.

It provides the control plane and plugins to make network storage
available via the Storage Performance Development Kit (SPDK) daemon,
both with hardware manager and without. Because SPDK runs in user
space and uses polling extensively, it achieves much better
performance and lower latency than comparable kernel drivers.

## Specification

This document specifies the roles, responsibilities and APIs of the
different components in this project. It gets converted to the
corresponding `oim.proto` automatically.

```protobuf
syntax = "proto3";
package oim.v0;

import "google/protobuf/wrappers.proto";

option go_package = "oim";
```

## OIM Registry

The OIM registry keeps track of manager hardware and the corresponding
OIM controller. It makes it possible to communicate with the OIM
controller when the components using the hardware and the controller
are in different networks by proxying requests.

Depending on the storage backend for the registry database, deployment
may consists of:
* a single instance when storing the registry in memory (only for
  testing purposes)
* a set of daemons when storing the registry in etcd

Even when deploying redundant OIM registry daemons, conceptually there
is only one OIM registry and thus this document only talks about one
"OIM registry".

The OIM registry provides the following gRPC APIs:

```protobuf
service Registry {
    // Adds a new entry to the registry DB or overwrites
    // an existing one.
    rpc RegisterController(RegisterControllerRequest)
        returns (RegisterControllerReply) {}
}

message RegisterControllerRequest {
    // An identifier for the OIM controller which is unique
    // among all controllers connected to the OIM registry.
    string controller_id = 1;
    // A string that can be used for grpc.Dial to connect
    // to the OIM controller.
    // See https://github.com/grpc/grpc/blob/master/doc/naming.md.
    // An empty string removes the database entry.
    string address = 2;
}

message RegisterControllerReply {
    // Intentionally empty.
}

// In addition, the Registry service also transparently proxies all
// unknown requests to the OIM controller if the request meta data
// contains a key "controllerid" with the ID string of a registered
// controller.
//
// If that key is missing, it replies with a gRPC "Unimplemented" error.
// If the controller is not currently registered, it replies with
// a gRPC "Unavailable" error.
```

## OIM Controller

There is one OIM controller per manager hardware device. It registers
the hardware with the OIM Registry on startup and at regular
intervals, to recover from a potential loss of the registry DB.

Once running, it responds to requests that control the operation of
the hardware.

```protobuf
service Controller {
    // Makes a volume available via the accelerator hardware.
    // The call must be idempotent: when a caller is unsure whether
    // a call was executed or what the result was, MapVolume
    // can be called again and will succeed without changing
    // anything.
    rpc MapVolume(MapVolumeRequest)
        returns (MapVolumeReply) {}

    // Removes access to the volume.
    // Also idempotent.
    rpc UnmapVolume(UnmapVolumeRequest)
        returns (UnmapVolumeReply) {}
}

message MapVolumeRequest {
    // An identifier for the volume that must be unique
    // among all volumes mapped by the OIM controller.
    // All calls with the same identifier must have the
    // same parameters.
    string volume_id = 1;
    // These parameters define how to access the volume.
    oneof params {
        MallocParams malloc = 2;
        CephParams ceph = 3;
    }
}

// For testing purposes a volume can be created in memory
// when it gets mapped for the first time. The data gets
// lost once the volume gets unmapped.
message MallocParams {
    // The desired size in bytes. Must be a multiple of 512.
    int64 size = 1;
}

// Defines a Ceph block device. This is currently a placeholder
// to demonstrate how MapVolumeRequest.params will work.
message CephParams {
    string secret = 1;
    // TODO: real ceph parameters
    // TODO: do not log secret in debug output
}

// The reply tells the caller enough about the mapped volume
// to access it.
message MapVolumeReply {
    // The PCI address of the controller which provides
    // access to the volume.
    string pci_address = 1;
    // The SCSI target and LUN in the format
    // x:y without extra zeros.
    // Only set for SCSI controllers.
    string scsi_device = 2;
}

message UnmapVolumeRequest {
    // The volume ID that was used when mapping the volume.
    string volume_id = 1;
}

message UnmapVolumeReply {
    // Intentionally empty.
}
```

## OIM CSI Driver

Connects to the OIM registry to find the OIM controller for the
hardware attached to the compute node. It uses that controller to
map or unmap volumes.

Implements https://github.com/container-storage-interface/spec/blob/master/spec.md
