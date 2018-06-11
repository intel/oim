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

Keeps track of manager hardware and the corresponding OIM controller.

One or more instances per cluster. No data cached in memory or on disk, all persistent or shared data stored in etcd.

Provides the following gRPC APIs:

```protobuf
service Registry {
    rpc RegisterController(RegisterControllerRequest)
        returns (RegisterControllerReply) {}
}

message RegisterControllerRequest {
    string UUID = 1;
    string address = 2;
}

message RegisterControllerReply {
    // Intentionally empty.
}
```

## OIM Controller

One per manager hardware device. Registers the hardware with the OIM
Registry and controls operation of the hardware.

```protobuf
service Controller {
    rpc MapVolume(MapVolumeRequest)
        returns (MapVolumeReply) {}

    rpc UnmapVolume(UnmapVolumeRequest)
        returns (UnmapVolumeReply) {}

}

message MapVolumeRequest {
    string UUID = 1;
    map<string, string> params = 2;
}

message MapVolumeReply {
    // Intentionally empty.
}

message UnmapVolumeRequest {
    string UUID = 1;
}

message UnmapVolumeReply {
    // Intentionally empty.
}
```

## OIM CSI Driver

Connects to the OIM Registry to find the OIM Controller for the
hardware attached to the compute node. Uses that Controller to
map or unmap volumes.

Implements https://github.com/container-storage-interface/spec/blob/master/spec.md
