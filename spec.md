# Specification

This document specifies the roles, responsibilities and APIs of the
different components in the Open Infrastructure Manager (OIM)
project. It gets converted to the corresponding `oim.proto`
automatically.

For an introduction of the components and concepts, see the main
[README.md](./README.md).

```protobuf
syntax = "proto3";
package oim.v0;

import "google/protobuf/wrappers.proto";

option go_package = "oim";
```

## OIM Registry

The OIM registry provides the following gRPC APIs:

```protobuf
service Registry {
    // Adds a new entry to the registry DB or overwrites
    // an existing one.
    rpc RegisterController(RegisterControllerRequest)
        returns (RegisterControllerReply) {}

    // Retrieves all registry DB entries.
    rpc GetControllers(GetControllerRequest)
        returns (GetControllerReply) {}
}

message RegisterControllerRequest {
    // An identifier for the OIM controller which is unique
    // among all controllers connected to the OIM registry.
    // The host name of each compute node might be used here
    // if it is known to be unique.
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

message GetControllerRequest {
    // Intentionally empty.
}

message GetControllerReply {
    // All current registry DB entries.
    repeated DBEntry entries = 1;
}

message DBEntry {
    // The unique key under which the OIM controller is registered.
    string controller_id = 1;
    // The grpc.Dial target for connecting to the OIM controller.
    string address = 2;
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

    // Creates or deletes (when size is zero) an
    // in-memory BDev for testing.
    rpc ProvisionMallocBDev(ProvisionMallocBDevRequest)
        returns (ProvisionMallocBDevReply) {}

    // Checks that the BDev exists. Returns
    // gRPC NOT_FOUND status if not.
    rpc CheckMallocBDev(CheckMallocBDevRequest)
        returns (CheckMallocBDevReply) {}
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

// For testing purposes, an existing Malloc BDev can be used.
// It needs to be provisioned separately to ensure that its
// data survives multiple Map/Unmap operations. It's name
// must be <volume_id>.
message MallocParams {
}

// Defines a Ceph block device. This is currently a placeholder
// to demonstrate how MapVolumeRequest.params will work.
message CephParams {
    string secret = 1;
    // TODO: real ceph parameters
    // TODO: do not log secret in debug output
}

// The reply tells the caller enough about the mapped volume
// to find it in /sys/dev/block.
message MapVolumeReply {
    // This string must be a substring of the symlink target
    // and has to be long enough to avoid mismatches.
    // Example: /devices/pci0000:00/0000:00:15.0/ for a QEMU
    // vhost SCSI device with bus=pci.0,addr=0x15
    string device = 1;
    // The SCSI target and LUN in the format
    // x:y without extra zeros. Empty for non-SCSI
    // controllers.
    string scsi = 2;
}

message UnmapVolumeRequest {
    // The volume ID that was used when mapping the volume.
    string volume_id = 1;
}

message UnmapVolumeReply {
    // Intentionally empty.
}

message ProvisionMallocBDevRequest {
    // The desired name of the new BDev.
    string bdev_name = 1;
    // The desired size in bytes. Must be a multiple of 512.
    int64 size = 2;
}

message ProvisionMallocBDevReply {
    // Intentionally empty.
}

message CheckMallocBDevRequest {
    // The name of an existing BDev.
    string bdev_name = 1;
}

message CheckMallocBDevReply {
    // Intentionally empty.
}
```

## OIM CSI Driver

Implements https://github.com/container-storage-interface/spec/blob/master/spec.md
