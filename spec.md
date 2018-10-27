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
    // Set or overwrite a registry DB entry.
    rpc SetValue(SetValueRequest)
        returns (SetValueReply) {}

    // Retrieves registry DB entries.
    rpc GetValues(GetValuesRequest)
        returns (GetValuesReply) {}
}

message SetValueRequest {
    Value value = 1;
}

// A single registry DB entry.
message Value {
    // A value is referenced by a set of path elements,
    // separated by slashes. Leading and trailing slashes
    // are ignored, repeated slashes treated like a single
    // slash. "." and ".." are invalid path elements.
    string path = 1;
    // The value itself is also a string.
    string value = 2;
}

message SetValueReply {
    // Intentionally empty.
}

message GetValuesRequest {
    // Return all values beneath or at the given path,
    // all values when empty.
    string path = 1;
}

message GetValuesReply {
    // All current registry DB values.
    repeated Value values = 1;
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

// Defines a Ceph block device.
message CephParams {
    // The user id (like "admin", but not "client.admin").
    // Can be left out, the default in Ceph is "admin".
    string user_id = 1;
    // The "key" value from a Ceph keyring for the user.
    string secret = 2;
    // Comma-separated list of addr:port values.
    string monitors = 3;
    // Pool name
    string pool = 4;
    // Image name
    string image = 5;
}

// The reply must tell the caller enough about the mapped volume
// to find it in /sys/dev/block.
message MapVolumeReply {
    // The PCI address (domain/bus/device/function, extended BDF).
    // A controller which does not know its own PCI address can
    // return a an address with all fields set to 0xFFFF.
    PCIAddress pci_address = 1;
    // The SCSI target and LUN. Only present for disks attached
    // via a SCSI controller.
    SCSIDisk scsi_disk = 2;
}

// Each field can be marked as unknown or unset with 0xFFFF.
// This leads to nicer code than the other workarounds for missing
// optional scalars (.google.protobuf.UInt32Value or oneof).
message PCIAddress {
    // Domain number.
    uint32 domain = 1;
    // Bus number.
    uint32 bus = 2;
    // Device number.
    uint32 device = 3;
    // Function number.
    uint32 function = 4;
}

message SCSIDisk {
    uint32 target = 1;
    uint32 lun = 2;
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
