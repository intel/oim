# Open Infrastructure Manager (OIM)

Open Infrastructure Manager (OIM) is an open source project which
simplifies the integration of storage and network accelerator hardware into
cloud environments like Kubernetes, Mesos and OpenStack.

It provides the control plane and plugins to make network storage
available via the
[Storage Performance Development Kit (SPDK)](http://www.spdk.io/)
daemon, both with hardware manager and without. Because SPDK runs in
user space and uses polling extensively, it achieves much better
performance and lower latency than comparable kernel drivers.


## Concepts

### Control Plane

OIM components may be on different networks. This is important for
storage accelerator hardware where the kernel on the main compute node
only sees a standard PCI device, like a SCSI controller, without a way
to issue special commands through that interface. In such a setup,
control of the accelerator hardware has to go through a proxy.

In OIM, this proxy is provided by the control plane where all accelerator
hardware is registered. This control plane is independent of any
particular cloud environment. Its implementation, the OIM registry,
and all other OIM components are implemented in Go. The resulting
binaries do not depend on any other userspace library and thus work on
different distributions.

For testing purposes the OIM registry can be run with a single process
storing all data in memory. A production environment is expected to
use [etcd](https://coreos.com/etcd/) with multiple OIM registry
instances as frontend to increase scalability. Communication with the
control plane is limited to short-lived, infrequent connections.

### Storage

The goal is to make existing network attached block volumes available
as local block devices. Depending on the cloud environment, either the
block devices or filesystems mounted on top of the devices will then
be used by applications.

Provisioning new block volumes is specific to the storage provider and
as such may require additional components that know how to control
certain storage providers.

### CSI

OIM implements a
[Container Storage Interface(CSI)](https://github.com/container-storage-interface/spec)
driver which can be used by any container orchestrator that supports
CSI. Operation as part of Kubernetes is specifically tested.

### Controller ID

Each accelerator hardware instance has its own controller. They get
identified by an ID that is unique for all instances connected to the
same control plane. This unique ID is used to route requests to the
right OIM controller via the OIM registry.

### Security

Eventually all communication will be protected by TLS when going over
a network and authentication/authorization will be added. How to
provision the necessary keys is under discussion.


## Components

### OIM Registry

The OIM registry keeps track of accelerator hardware and the corresponding
OIM controller. It makes it possible to communicate with the OIM
controller when the components using the hardware and the controller
are in different networks by proxying requests.

Depending on the storage backend for the registry database, deployment
may consists of:
* a single instance when storing the registry in memory (only for
  testing purposes)
* a set of daemons when storing the registry in etcd

Even when deploying redundant OIM registry daemons, conceptually there
is only one OIM registry.

### OIM Controller

There is one OIM controller per accelerator hardware device. It registers
the hardware with the OIM Registry on startup and at regular
intervals, to recover from a potential loss of the registry DB.

Once running, it responds to requests that control the operation of
the hardware.

### OIM CSI Driver

Connects to the OIM registry to find the OIM controller for the
hardware attached to the compute node. It uses that controller to
map or unmap volumes.

### SPDK

The [SPDK vhost daemon](http://www.spdk.io/doc/vhost.html) is used to
access network attached block volumes via protocols like iSCSI, Ceph,
or NVMeOF. The OIM controller reconfigures SPDK such that these block
devices appear as new disks on the local node.

When used without special accelerator hardware, SPDK and OIM CSI driver
run on the same Linux kernel. The driver controls the SPDK daemon
directly and OIM registry and controller are not needed. Volumes are
made available via that Linux kernel as network block devices
([NBD](https://nbd.sourceforge.io/)) with SPDK handling the actual
network communication.

When used with accelerator hardware, SPDK runs on separate hardware and
the volumes appear as new SCSI devices on the same virtio SCSI
controller (VHost SCSI). In the future, dynamically attaching new
virtio block controllers for each new volume might also be supported.


## Building

All that is needed for building the core OIM components is a recent Go
toolchain (>= 1.7 should be enough), `bash` and standard POSIX
tools. Building therefore should work on Linux distros, Mac OS X and
even Windows when those additional tools are installed. Testing will
need additional packages, which will be described in the corresponding
sections below.

This repository _**must**_ be checked out at
`$GOPATH/src/github.com/intel/oim`. Then the
top-level Makefile can be used to produce binaries under _work:

    make all

See the [Makefile](./Makefile) for additional make targets.

## Proxy settings

When moving beyond simple building, external resources like images
from Clear Linux or Docker registries are needed. In a corporate
environment where proxies have to be used for HTTP, the following
environment variables will be used to configure proxy usage:

- http_proxy or HTTP_PROXY
- https_proxy or HTTPS_PROXY
- no_proxy or NO_PROXY

Note that Go will try to use the HTTP proxy also for local connections
to `0.0.0.0`, which cannot work. `no_proxy` must contain `0.0.0.0` to
prevent this. The Makefile will add that automatically, but when
invoking test commands directly, it has to be added to `no_proxy`
manually.

When setting up the virtual machine (see below), the proxy env
variables get copied into the virtual machine's
`/etc/systemd/system/docker.service.d/oim.conf` file at the time of
creating the image file. When changing proxy settings later, that file
has to be updated manually (see below for instructions for starting
and logging into the virtual machine) or the image must be created
again after a `make clean`.


## Testing

Simple tests can be run with `make`, `go test` or `dlv test` and don't
have any additional dependencies:

    cd pkg/oim-csi-driver && go test
    make test

More complex tests involve QEMU and SPDK and must be explicitly
enabled as explained in the following sections.

`make test` invokes `go test`, and that command caches test results.
It only runs tests anew if the Go source code was modified since the
last test run. When tests need to be run again because the external
resources (QEMU image, SPDK) or the test configuration variables
changed, then one has to clean the cache first or use a special make
target:

    go clean -testcache
    make force_test

### QEMU + Kubernetes

The `qemu-system-x86_64` binary must be installed, either from
[upstream QEMU](https://www.qemu.org/) or the Linux distribution. The
version must be v2.10.0 or higher because vhost-scsi is required
([SPDK Prerequisites](http://www.spdk.io/doc/vhost.html#vhost_prereqs)).
KVM must be enabled and the user must be allowed to use it. Usually this
is done by adding the user to the `kvm` group. The
["Install QEMU-KVM"](https://clearlinux.org/documentation/clear-linux/get-started/virtual-machine-install/kvm)
section in the Clear Linux documentation contains further information
about enabling KVM and installing QEMU.

To ensure that QEMU and KVM are working, run this:

    make _work/clear-kvm-original.img _work/start-clear-kvm _work/OVMF.fd
    cp _work/clear-kvm-original.img _work/clear-kvm-test.img
    _work/start-clear-kvm _work/clear-kvm-test.img

The result should be login prompt like this:

    [    0.049839] kvm: no hardware support
    
    clr-c3f99095d2934d76a8e26d2f6d51cb91 login: 

The message about missing KVM hardware support comes from inside the
virtual machine and indicates that nested KVM is not enabled. This can
be ignored because it is not needed.

Now the running QEMU can be killed and the test image removed again:

    killall qemu-system-x86_64 # in a separate shell
    rm _work/clear-kvm-test.img
    reset # Clear Linux changes terminal colors, undo that.

Testing with QEMU can be enabled in several different ways. When using
make, these variables can be set in the environment or via the make
parameters.

    make test TEST_QEMU_IMAGE=_work/clear-kvm.img
    cd pkg/qemu && TEST_QEMU_IMAGE=<full path>/_work/clear-kvm.img go test

The `clear-kvm.img` image itself is prepared
automatically by the Makefile. It will contain the latest
[Clear Linux OS](https://clearlinux.org/) and have the latest stable
Kubernetes installed on it. This can be used also stand-alone:

    make _work/clear-kvm.img
    _work/start-clear-kvm _work/clear-kvm.img >/dev/null </dev/null &
    _work/kube-clear-kvm
    _work/ssh-clear-kvm kubectl get nodes
    kubectl --kubeconfig _work/clear-kvm-kube.config get nodes
    _work/ssh-clear-kvm shutdown now

Some ports are hard-coded in the startup script, so only a single instance of
the virtual machine is ever used during testing although tests are running
in parallel. Those ports also must be available on the build machine.

### SPDK

Running SPDK is [not possible without root privileges](https://github.com/spdk/spdk/issues/314). Mounting
devices also requires root privileges. The impact has been minimized as much as
possible by running most code as normal user in the developer's
environment and only invoking some operations via sudo.

The build machine must be prepared to allow this, huge pages must be
set up so that normal users can access them, and nbd must be available:
* [`sudo`](https://en.wikipedia.org/wiki/Sudo) must be able to run arbitrary commands
* `sudo env PCI_WHITELIST="none" vendor/github.com/spdk/spdk/scripts/setup.sh && sudo chmod a+rw /dev/hugepages`
* `sudo modprobe nbd`

Building SPDK depends on additional packages. SPDK
[provides a shell script](https://github.com/spdk/spdk/blob/master/README.md#prerequisites)
for installing those. In OIM, that script can be invoked as:

    sudo ./vendor/github.com/spdk/spdk/scripts/pkgdep.sh

SPDK will be built automatically from known-good source code bundled in the repository
when selecting it with:

    make test TEST_SPDK_VHOST_BINARY=_work/vhost

Alternatively, one can build and start SPDK manually and then just point
to the RPC socket. For example, this way one can debug and/or monitor SPDK in
more detail by wrapping it in `strace`:

    sudo app/vhost/vhost -S /tmp -r /tmp/spdk.sock & sleep 1; sudo chmod a+rw /tmp/spdk.sock*; sudo strace -t -v -p `pidof vhost` -e 'trace=!getrusage' -s 256 2>&1 | tee /tmp/full.log | grep -v EAGAIN & fg %-
    make test TEST_SPDK_VHOST_SOCKET=/tmp/spdk.sock

### Docker

The e2e tests that get enabled when specifying a QEMU image depend on
a OIM CSI driver image in a local Docker registry and thus need:
* Docker
* a local [Docker registry](https://docs.docker.com/registry/deploying/)

The driver image will be built automatically by the `Makefile` when running
the tests. It can also be built separately with:

    make push-oim-csi-driver

### Full testing

In addition to enabling additional tests individually as explained
above, it is also possible to enable everything at once with:

    make test WITH_E2E_TESTS=1


## Troubleshooting

### Missing host setup

SPDK startup failures are usually a result of not running the `setup.sh` script:

    2018/06/28 11:48:20 Starting /fast/work/gopath/src/github.com/intel/oim/_work/vhost
    2018/06/28 11:48:20 spdk: Starting SPDK v18.07-pre / DPDK 18.02.0 initialization...
    2018/06/28 11:48:20 spdk: [ DPDK EAL parameters: vhost -c 0x1 -m 256 --file-prefix=spdk_pid297515 ]
    2018/06/28 11:48:20 spdk: EAL: Detected 44 lcore(s)
    2018/06/28 11:48:20 spdk: EAL: No free hugepages reported in hugepages-2048kB
    2018/06/28 11:48:20 spdk: EAL: No free hugepages reported in hugepages-1048576kB
    2018/06/28 11:48:20 spdk: EAL: FATAL: Cannot get hugepage information.
    2018/06/28 11:48:20 spdk: EAL: Cannot get hugepage information.
    2018/06/28 11:48:20 spdk: Failed to initialize DPDK
    2018/06/28 11:48:20 spdk: app.c: 430:spdk_app_setup_env: *ERROR*: Unable to initialize SPDK env
    Jun 28 11:48:23.903: INFO: Failed to setup provider config: Timed out waiting for /tmp/spdk853524971/spdk.sock
    Failure [3.002 seconds]
    [BeforeSuite] BeforeSuite
    /fast/work/gopath/src/github.com/intel/oim/test/e2e/e2e.go:167

      Jun 28 11:48:23.903: Failed to setup provider config: Timed out waiting for /tmp/spdk853524971/spdk.sock


To prepare the host for testing one has to run:

    sudo env PCI_WHITELIST="none" vendor/github.com/spdk/spdk/scripts/setup.sh && \
    sudo chmod a+rw /dev/hugepages && \
    sudo modprobe nbd

### Orphaned QEMU

    Jun 28 12:02:12.585: Failed to setup provider config: Problem with QEMU [/fast/work/gopath/src/github.com/intel/oim/_work/start-clear-kvm /fast/work/gopath/src/github.com/intel/open-infrastructure-manager/_work/clear-kvm.img -serial none -chardev stdio,id=mon0 -serial file:/fast/work/gopath/src/github.com/intel/open-infrastructure-manager/_work/serial.log -mon chardev=mon0,mode=control,pretty=off -object memory-backend-file,id=mem,size=1024M,mem-path=/dev/hugepages,share=on -numa node,memdev=mem -m 1024 -chardev socket,id=vhost0,path=/tmp/spdk058450750/e2e-test-vhost -device vhost-user-scsi-pci,id=scsi0,chardev=vhost0,bus=pci.0,addr=0x15]: EOF
      Command terminated: exit status 1
      qemu-system-x86_64: Could not set up host forwarding rule 'tcp::16443-:6443'

When QEMU fails to start up like this, check for any remaining `qemu-system-x86_64` instance and kill it:

    killall qemu-system-x86_64

### KVM permissions

    Could not access KVM kernel module: Permission denied

Typically, the user trying to use KVM must part of the `kvm` group.

### Missing Docker

    docker push localhost:5000/oim-csi-driver:canary
    The push refers to repository [localhost:5000/oim-csi-driver]
    Get http://localhost:5000/v2/: dial tcp [::1]:5000: connect: connection refused
    make: *** [push-oim-csi-driver] Error 1

A Docker registry is expected to be set up on the localhost. If it runs elsewhere
or listens on a different port, then `make REGISTRY_NAME=<host:port>`
can be used to override the default.

### Incomplete `no_proxy`

    INFO oim-registry: listening for connections | address: 0.0.0.0:32793
    INFO Registering OIM controller controller-registration-test-2 at address foo://bar with OIM registry 0.0.0.0:32793
    DEBUG sending | method: /oim.v0.Registry/RegisterController request: controller_id:"controller-registration-test-2" address:"foo://bar" 
    ERROR received | method: /oim.v0.Registry/RegisterController error: rpc error: code = Unavailable desc = all SubConns are in TransientFailure, latest connection error: connection error: desc = "transport: Error while dialing failed to do connect handshake, response: \"HTTP/1.1 403 Forbidden...

`http_proxy` was set but `no_proxy` did not contain `0.0.0.0`, so Go
tried to connect to the local service via the HTTP proxy.
