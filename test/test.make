# By default, testing only runs tests that work without additional components.
# Additional tests can be enabled by overriding the following makefile variables
# or (when invoking go test manually) by setting the corresponding env variables

# Unix domain socket path of a running SPDK vhost.
# TEST_SPDK_VHOST_SOCKET=

# Alternatively, the path to a spdk/app/vhost binary can be provided.
# Use "_work/vhost" and that binary will be built automatically in _work.
# TEST_SPDK_VHOST_BINARY=

# Image base name to boot under QEMU before running tests, for example
# "_work/clear-kvm.img".
# TEST_QEMU_IMAGE=

# If Ginkgo is available, then testing can be sped up by using
# TEST_CMD=ginkgo -p.
TEST_CMD=go test -timeout 0
TEST_ALL=$(IMPORT_PATH)/pkg/... $(IMPORT_PATH)/test/e2e
TEST_ARGS=$(IMPORT_PATH)/pkg/... $(if $(_TEST_QEMU_IMAGE), $(IMPORT_PATH)/test/e2e)

.PHONY: test
test: all vet run_tests

# TODO: add -shadow
.PHONY: vet
vet:
	go vet $(IMPORT_PATH)/pkg/... $(IMPORT_PATH)/cmd/...

# golint may sometimes be too strict and intentionally has
# no way to suppress uninteded warnings, but it finds real
# issues, so we try to become complete free of warnings.
# Right now only some parts of the code pass, so the test
# target only checks those to avoid regressions.
#
# golint might not be installed, so we skip the test if
# that is the case.
.PHONY: lint test_lint
test: test_lint
lint:
	golint $(IMPORT_PATH)/pkg/... $(IMPORT_PATH)/cmd/...
test_lint:
	@ golint -help >/dev/null 2>&1; if [ $$? -eq 2 ]; then echo "running golint..."; golint -set_exit_status $(IMPORT_PATH)/pkg/log; fi

# Check resp. fix formatting.
.PHONY: test_fmt fmt
test: test_fmt
test_fmt:
	@ files=$$(find pkg cmd test -name '*.go'); \
	if [ $$(gofmt -d $$files | wc -l) -ne 0 ]; then \
		echo "formatting errors:"; \
		gofmt -d $$files; \
		false; \
	fi
fmt:
	gofmt -l -w $$(find pkg cmd test -name '*.go')

# Determine whether we have QEMU and SPDK.
_TEST_QEMU_IMAGE=$(if $(TEST_QEMU_IMAGE),$(TEST_QEMU_IMAGE),$(if $(WITH_E2E_TESTS),_work/clear-kvm.img))
_TEST_SPDK_VHOST_BINARY=$(if $(TEST_SPDK_VHOST_BINARY),$(TEST_SPDK_VHOST_BINARY),$(if $(WITH_E2E_TESTS),_work/vhost))

# Derive filenames of helper files from QEMU image name.
TEST_QEMU_PREFIX=$(if $(_TEST_QEMU_IMAGE),$(dir $(_TEST_QEMU_IMAGE))$(1)-$(notdir $(basename $(_TEST_QEMU_IMAGE))))
TEST_QEMU_START=$(call TEST_QEMU_PREFIX,start)
TEST_QEMU_SSH=$(call TEST_QEMU_PREFIX,ssh)
TEST_QEMU_KUBE=$(call TEST_QEMU_PREFIX,kube)
TEST_QEMU_DEPS=$(_TEST_QEMU_IMAGE) $(TEST_QEMU_START) $(TEST_QEMU_SSH) $(TEST_QEMU_KUBE)

# We only need to build and push the latest OIM CSI driver if we actually use it during testing.
TEST_E2E_DEPS=$(if $(filter $(IMPORT_PATH)/test/e2e, $(TEST_ARGS)), push-oim-csi-driver)

.PHONY: run_tests
run_tests: $(TEST_QEMU_DEPS) $(_TEST_SPDK_VHOST_BINARY) $(TEST_E2E_DEPS) oim-csi-driver
	TEST_OIM_CSI_DRIVER_BINARY=$(abspath _output/oim-csi-driver) \
	TEST_SPDK_VHOST_SOCKET=$(abspath $(TEST_SPDK_VHOST_SOCKET)) \
	TEST_SPDK_VHOST_BINARY=$(abspath $(_TEST_SPDK_VHOST_BINARY)) \
	TEST_QEMU_IMAGE=$(abspath $(_TEST_QEMU_IMAGE)) \
	    $(TEST_CMD) $(shell go list $(TEST_ARGS) | sed -e 's;$(IMPORT_PATH);./;' )

.PHONY: force_test
force_test: clean_testcache test

# go caches test results. If we want to rerun tests because e.g. SPDK
# was restarted, then we must throw away cached results first.
.PHONY: clean_testcache
clean_testcache:
	go clean -testcache

# oim-registry and oim-controller should not contain glog, while
# oim-csi-driver still does (via Kubernetes).
.PHONY: test_no_glog
test: test_no_glog
test_no_glog: oim-controller oim-registry
	@ for i in $+; do if _output/$$i --help 2>&1 | grep -q -e -alsologtostderr; then echo "ERROR: $$i contains glog!"; exit 1; fi; done

.PHONY: coverage
coverage:
	mkdir -p _work
	go test -coverprofile _work/cover.out $(IMPORT_PATH)/pkg/...
	go tool cover -html=_work/cover.out -o _work/cover.html

# This ensures that the vendor directory and vendor-bom.csv are in sync
# at least as far as the listed components go.
.PHONY: test_vendor_bom
test: test_vendor_bom
test_vendor_bom:
	@ if ! diff -c \
		<(tail +2 vendor-bom.csv | sed -e 's/;.*//') \
		<((grep '^  name =' Gopkg.lock  | sed -e 's/.*"\(.*\)"/\1/'; echo github.com/dpdk/dpdk) | sort); then \
		echo; \
		echo "vendor-bom.csv not in sync with vendor directory (aka Gopk.lock):"; \
		echo "+ new entry, missing in vendor-bom.csv"; \
		echo "- obsolete entry in vendor-bom.csv"; \
		false; \
	fi

# This ensures that we know about all components that are needed at
# runtime on a production system. Those must be scrutinized more
# closely than components that are merely needed for testing.
#
# Intel has a process for this. The mapping from import path to "name"
# + "download URL" must match how the components are identified at
# Intel while reviewing the components.
.PHONY: test_runtime_deps
test: test_runtime_deps
test_runtime_deps:
	@ if ! diff -c \
		runtime-deps.csv \
		<( $(RUNTIME_DEPS) ); then \
		echo; \
		echo "runtime-deps.csv not up-to-date. Update RUNTIME_DEPS in test/test.make, rerun, review and finally apply the patch above."; \
		false; \
	fi

RUNTIME_DEPS =

# We use "go list" because it is readily available. A good replacement
# would be godeps. We list dependencies recursively, not just the
# direct dependencies.
RUNTIME_DEPS += go list -f '{{ join .Deps "\n" }}' $(foreach cmd,$(OIM_CMDS),./cmd/$(cmd)) |

# This focuses on packages that are not in Golang core.
RUNTIME_DEPS += grep '^github.com/intel/oim/vendor/' |

# Filter out some packages that aren't really code.
RUNTIME_DEPS += grep -v -e 'github.com/container-storage-interface/spec' |
RUNTIME_DEPS += grep -v -e 'google.golang.org/genproto/googleapis/rpc/status' |

# Reduce the package import paths to project names + download URL.
# - strip prefix
RUNTIME_DEPS += sed -e 's;github.com/intel/oim/vendor/;;' |
# - use path inside github.com as project name
RUNTIME_DEPS += sed -e 's;^github.com/\([^/]*\)/\([^/]*\).*;github.com/\1/\2;' |
# - everything from gRPC is one project
RUNTIME_DEPS += sed -e 's;google.golang.org/grpc/*.*;grpc-go,https://github.com/grpc/grpc-go;' |
# - various other projects
RUNTIME_DEPS += sed \
	-e 's;github.com/gogo/protobuf;gogo protobuf,https://github.com/gogo/protobuf;' \
	-e 's;github.com/golang/glog;glog,https://github.com/golang/glog;' \
	-e 's;github.com/pkg/errors;pkg/errors,https://github.com/pkg/errors;' \
	-e 's;github.com/vgough/grpc-proxy;grpc-proxy,https://github.com/vgough/grpc-proxy;' \
	-e 's;golang.org/x/.*;Go,https://github.com/golang/go;' \
	-e 's;k8s.io/.*;kubernetes,https://github.com/kubernetes/kubernetes;' \
	-e 's;gopkg.in/fsnotify.*;golang-github-fsnotify-fsnotify,https://github.com/fsnotify/fsnotify;' \
	| cat |

# Ignore duplicates.
RUNTIME_DEPS += sort -u

# Downloads and unpacks the latest Clear Linux KVM image.
# This intentionally uses a different directory, otherwise
# we would end up sending the KVM images to the Docker
# daemon when building new Docker images as part of the
# build context.
#
# Sets the image up so that "ssh" works as root with a random
# password (stored in _work/passwd) and with _work/id as
# new private ssh key.
#
# Using chat for this didn't work because chat connected to
# qemu via pipes complained about unsupported ioctls. Expect
# would have been another alternative, but wasn't tried.
#
# Using plain bash might be a bit more brittle and harder to read, but
# at least it avoids extra dependencies.  Inspired by
# http://wiki.bash-hackers.org/syntax/keywords/coproc
#
# A registry on the build host (i.e. localhost:5000) is marked
# as insecure in Clear Linux under the hostname of the build host.
# Otherwise pulling images fails.
#
# The latest upstream Kubernetes binaries are used because that way
# the resulting installation is always up-to-date. Some workarounds
# in systemd units are necessary to get that up and running.
#
# The resulting cluster has:
# - a single node with the master taint removed
# - networking managed by kubelet itself
#
# Kubernetes does not get started by default because it might
# not always be needed in the image, depending on the test.
# _work/kube-clear-kvm can be used to start it.

# Sanitize proxy settings (accept upper and lower case, set and export upper
# case) and add local machine to no_proxy because some tests may use a
# local Docker registry. Also exclude 0.0.0.0 because otherwise Go
# tests using that address try to go through the proxy.
HTTP_PROXY=$(shell echo "$${HTTP_PROXY:-$${http_proxy}}")
HTTPS_PROXY=$(shell echo "$${HTTPS_PROXY:-$${https_proxy}}")
NO_PROXY=$(shell echo "$${NO_PROXY:-$${no_proxy}},$$(ip addr | grep inet6 | grep /64 | sed -e 's;.*inet6 \(.*\)/64 .*;\1;' | tr '\n' ','; ip addr | grep -w inet | grep /24 | sed -e 's;.*inet \(.*\)/24 .*;\1;' | tr '\n' ',')",192.168.7.1,0.0.0.0,10.0.2.15)
export HTTP_PROXY HTTPS_PROXY NO_PROXY
PROXY_ENV=env 'HTTP_PROXY=$(HTTP_PROXY)' 'HTTPS_PROXY=$(HTTPS_PROXY)' 'NO_PROXY=$(NO_PROXY)'

_work/clear-kvm-original.img:
	$(DOWNLOAD_CLEAR_IMG)

# This picks the latest available version. Can be overriden via make CLEAR_IMG_VERSION=
CLEAR_IMG_VERSION = $(shell curl https://download.clearlinux.org/latest)

DOWNLOAD_CLEAR_IMG = true
DOWNLOAD_CLEAR_IMG += && mkdir -p _work
DOWNLOAD_CLEAR_IMG += && cd _work
DOWNLOAD_CLEAR_IMG += && dd if=/dev/random bs=1 count=8 2>/dev/null | od -A n -t x8 | sed -e 's/ //g' >passwd
DOWNLOAD_CLEAR_IMG += && version=$(CLEAR_IMG_VERSION)
DOWNLOAD_CLEAR_IMG += && [ "$$version" ]
DOWNLOAD_CLEAR_IMG += && curl -O https://download.clearlinux.org/releases/$$version/clear/clear-$$version-kvm.img.xz
DOWNLOAD_CLEAR_IMG += && curl -O https://download.clearlinux.org/releases/$$version/clear/clear-$$version-kvm.img.xz-SHA512SUMS
DOWNLOAD_CLEAR_IMG += && curl -O https://download.clearlinux.org/releases/$$version/clear/clear-$$version-kvm.img.xz-SHA512SUMS.sig
# skipping image verification, does not work at the moment (https://github.com/clearlinux/distribution/issues/85)
# DOWNLOAD_CLEAR_IMG += && openssl smime -verify -in clear-$$version-kvm.img.xz-SHA512SUMS.sig -inform der -content clear-$$version-kvm.img.xz-SHA512SUMS -CAfile ../test/ClearLinuxRoot.pem -out /dev/null
DOWNLOAD_CLEAR_IMG += && sed -e 's;/.*/;;' clear-$$version-kvm.img.xz-SHA512SUMS | sha512sum -c
DOWNLOAD_CLEAR_IMG += && unxz -c <clear-$$version-kvm.img.xz >clear-kvm-original.img

# Number of nodes to be created in the virtual cluster, including master node.
NUM_NODES = 4

# Multiple different images can be created, starting with clear-kvm.0.img
# and ending with clear-kvm.<NUM_NODES - 1>.img.
#
# They have fixed IP addresses starting with 192.168.7.2 and host names
# kubernetes-0/1/2/.... The first image is for the Kubernetes master node,
# but configured so that also normal apps can run on it, i.e. no additional
# worker nodes are needed.
_work/clear-kvm.img: test/setup-clear-kvm.sh _work/clear-kvm-original.img _work/kube-clear-kvm _work/OVMF.fd _work/start-clear-kvm _work/id
	$(PROXY_ENV) test/setup-clear-kvm.sh $(NUM_NODES)
	ln -sf clear-kvm.0.img $@

_work/start-clear-kvm: test/start_qemu.sh
	mkdir -p _work
	cp $< $@
	sed -i -e "s;\(OVMF.fd\);$$(pwd)/_work/\1;g" $@
	chmod a+x $@

_work/kube-clear-kvm: test/start_kubernetes.sh
	mkdir -p _work
	cp $< $@
	sed -i -e "s;SSH;$$(pwd)/_work/ssh-clear-kvm;g" $@
	chmod u+x $@

_work/OVMF.fd:
	mkdir -p _work
	curl -o $@ https://download.clearlinux.org/image/OVMF.fd

_work/id:
	mkdir -p _work
	ssh-keygen -N '' -f $@

.PHONY: test_protobuf
test: test_protobuf
test_protobuf:
	@ if go list -f '{{ join .Deps "\n" }}' $(foreach i,$(OIM_CMDS),./cmd/$(i)) | grep -q github.com/golang/protobuf; then \
		echo "binaries should not depend on golang/protobuf, use gogo/protobuf instead"; \
		false; \
	fi

# Brings up the emulator environment:
# - starts the OIM control plane and SPDK on the local host
# - creates an SPDK virtio-scsi controller
# - starts a QEMU virtual machine connected to SPDK's virtio-scsi controller
# - starts a Kubernetes cluster
# - deploys the OIM driver
start: _work/clear-kvm.img _work/kube-clear-kvm _work/start-clear-kvm _work/ssh-clear-kvm
	if ! [ -e _work/oim-registry.pid ] || ! kill -0 $$(cat _work/oim-registry.pid) 2>/dev/null; then \
		truncate -s 0 _work/oim-registry.log && \
		( _output/oim-registry -endpoint tcp://192.168.7.1:0 -log.level DEBUG >>_work/oim-registry.log 2>&1 & echo $$! >_work/oim-registry.pid ) && \
		while ! grep -m 1 'listening for connections' _work/oim-registry.log > _work/oim-registry.port; do sleep 1; done && \
		sed -i -e 's/.*address: .*://' _work/oim-registry.port; \
	fi
	if ! [ -e _work/vhost.pid ] || ! sudo kill -0 $$(cat _work/vhost.pid) 2>/dev/null; then \
		rm -rf _work/vhost-run _work/vhost.pid && \
		mkdir _work/vhost-run && \
		( sudo _work/vhost -R -S $$(pwd)/_work/vhost-run -r $$(pwd)/_work/vhost-run/spdk.sock -f _work/vhost.pid -s 256 >_work/vhost.log 2>&1 & while ! [ -s _work/vhost.pid ] || ! [ -e _work/vhost-run/spdk.sock ]; do sleep 1; done ) && \
		sudo chmod a+rw _work/vhost-run/spdk.sock; \
	fi
	if ! [ -e _work/vhost-run/scsi0 ]; then \
		vendor/github.com/spdk/spdk/scripts/rpc.py -s $$(pwd)/_work/vhost-run/spdk.sock construct_vhost_scsi_controller scsi0 && \
		while ! [ -e _work/vhost-run/scsi0 ]; do sleep 1; done && \
		sudo chmod a+rw _work/vhost-run/scsi0; \
	fi
	if ! [ -e _work/oim-controller.pid ] || ! kill -0 $$(cat _work/oim-controller.pid) 2>/dev/null; then \
		( _output/oim-controller -endpoint unix://$$(pwd)/_work/oim-controller.sock \
		                         -spdk _work/vhost-run/spdk.sock \
		                         -vhost-scsi-controller scsi0 \
		                         -vm-vhost-device /devices/pci0000:00/0000:00:15.0/ \
		                         -log.level DEBUG \
		                         >_work/oim-controller.log 2>&1 & echo $$! >_work/oim-controller.pid ) && \
		while ! grep -q 'listening for connections' _work/oim-controller.log; do sleep 1; done; \
	fi
	_output/oimctl -registry 192.168.7.1:$$(cat _work/oim-registry.port) \
		-set "host-0=unix://$$(pwd)/_work/oim-controller.sock"
	for i in $$(seq 0 $$(($(NUM_NODES) - 1))); do \
		if ! [ -e _work/clear-kvm.$$i.pid ] || ! kill -0 $$(cat _work/clear-kvm.$$i.pid) 2>/dev/null; then \
			if [ $$i -eq 0 ]; then \
				opts="-m 2048 -object memory-backend-file,id=mem,size=2048M,mem-path=/dev/hugepages,share=on -numa node,memdev=mem -chardev socket,id=vhost0,path=_work/vhost-run/scsi0 -device vhost-user-scsi-pci,id=scsi0,chardev=vhost0,bus=pci.0,addr=0x15"; \
			else \
				opts=; \
			fi; \
			_work/start-clear-kvm _work/clear-kvm.$$i.img -monitor none -serial file:/nvme/gopath/src/github.com/intel/oim/_work/clear-kvm.$$i.log $$opts & \
			echo $$! >_work/clear-kvm.$$i.pid; \
		fi; \
	done
	_work/kube-clear-kvm
	for i in malloc-rbac.yaml malloc-storageclass.yaml malloc-daemonset.yaml; do \
		cat deploy/kubernetes/malloc/$$i | \
			sed -e "s;@OIM_REGISTRY_ADDRESS@;192.168.7.1:$$(cat _work/oim-registry.port);" | \
			_work/ssh-clear-kvm kubectl create -f - || true; \
	done
	@ echo "The test cluster is ready. Log in with _work/ssh-clear-kvm, run kubectl once logged in."
	@ echo "To try out the OIM CSI driver:"
	@ echo "   cat deploy/kubernetes/malloc/example/malloc-pvc.yaml | _work/ssh-clear-kvm kubectl create -f -"
	@ echo "   cat deploy/kubernetes/malloc/example/malloc-app.yaml | _work/ssh-clear-kvm kubectl create -f -"

stop:
	if [ -e _work/clear-kvm.0.pid ]; then \
		for i in example/malloc-app.yaml example/malloc-pvc.yaml malloc-rbac.yaml malloc-storageclass.yaml malloc-daemonset.yaml; do \
			cat deploy/kubernetes/malloc/$$i | _work/ssh-clear-kvm kubectl delete -f - || true; \
		done; \
	fi
	for i in $$(seq 0 $$(($(NUM_NODES) - 1))); do \
		if [ -e _work/clear-kvm.$$i.pid ]; then \
			kill -9 $$(cat _work/clear-kvm.$$i.pid) 2>/dev/null; \
			rm -f _work/clear-kvm.$$i.pid; \
		fi; \
	done
	if [ -e _work/oim-registry.pid ]; then \
		kill -9 $$(cat _work/oim-registry.pid) 2>/dev/null; \
		rm -f _work/oim-registry.pid; \
	fi
	if [ -e _work/oim-controller.pid ]; then \
		kill -9 $$(cat _work/oim-controller.pid) 2>/dev/null; \
		rm -f _work/oim-controller.pid; \
	fi
	if [ -e _work/vhost.pid ]; then \
		if grep -q vhost /proc/$$(cat _work/vhost.pid)/cmdline; then \
			sudo kill $$(cat _work/vhost.pid) 2>/dev/null; \
		fi; \
		rm -f _work/vhost.pid; \
	fi
