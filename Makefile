# Copyright 2017 The Kubernetes Authors.
# Copyright 2018 Intel Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

IMPORT_PATH=github.com/intel/oim

REGISTRY_NAME=localhost:5000
IMAGE_VERSION_oim-csi-driver=canary
IMAGE_TAG=$(REGISTRY_NAME)/$*:$(IMAGE_VERSION_$*)

OIM_CMDS=oim-controller oim-csi-driver oim-registry

# Build main set of components.
.PHONY: all
all: $(OIM_CMDS)

# Run operations only developers should need after making code changes.
update: update_dep

# We have to do some post-processing because dep does not support
# go-bindata.
update_dep: test/gobindata_util.go.patch
	dep ensure -v
	patch vendor/k8s.io/kubernetes/test/e2e/generated/gobindata_util.go <$<

.PHONY: update update_dep

# By default, testing only runs tests that work without additional components.
# Additional tests can be enabled by overriding the following makefile variables
# or (when invoking go test manually) by setting the corresponding env variables

# Unix domain socket path of a running SPDK vhost.
TEST_SPDK_VHOST_SOCKET=

# Image base name to boot under QEMU before running tests, for example
# "clear-kvm".
TEST_QEMU_IMAGE=

# Disabling parallelism is important, because the QEMU virtual machine and SPDK
# are shared between different packages.
# TODO: start app/vhost per test, dynamically choose ssh port for QEMU
TEST_CMD=go test -v -p 1
TEST_ARGS=$(IMPORT_PATH)/pkg/...

.PHONY: test
test: all vet run_tests

.PHONY: vet
vet:
	go vet $(IMPORT_PATH)/pkg/... $(IMPORT_PATH)/cmd/...

.PHONY: run_tests
run_tests: $(patsubst %, _work/%.img, $(TEST_QEMU_IMAGE)) $(patsubst %, _work/start-%, $(TEST_QEMU_IMAGE)) $(patsubst %, _work/ssh-%, $(TEST_QEMU_IMAGE))
	mkdir -p _work
	cd _work && \
	TEST_SPDK_VHOST_SOCKET=$(TEST_SPDK_VHOST_SOCKET) \
	TEST_QEMU_IMAGE=$(addprefix $$(pwd)/, $(TEST_QEMU_IMAGE)) \
	    $(TEST_CMD) $(TEST_ARGS)

.PHONY: force_test
force_test: clean_testcache test

# go caches test results. If we want to rerun tests because e.g. SPDK
# was restarted, then we must throw away cached results first.
.PHONY: clean_testcache
clean_testcache:
	go clean -testcache

.PHONY: coverage
coverage:
	mkdir -p _work
	go test -coverprofile _work/cover.out $(IMPORT_PATH)/pkg/...
	go tool cover -html=_work/cover.out -o _work/cover.html

.PHONY: $(OIM_CMDS)
$(OIM_CMDS):
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o _output/$@ ./cmd/$@

# _output is used as the build context. All files inside it are sent
# to the Docker daemon when building images.
%-container: %
	cp cmd/$*/Dockerfile _output/Dockerfile.$*
	cd _output && docker build -t $(IMAGE_TAG) -f Dockerfile.$* .

push-%: %-container
	docker push $(IMAGE_TAG)

.PHONY: clean
clean:
	go clean -r -x
	-rm -rf _output _work

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
# rngd gets installed (from the cryptography bundle) and enabled
# because it was observed that starting docker hangs due to
# lack of entropy directly after starting a virtual machine.
# https://github.com/clearlinux/distribution/issues/97
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
# - weave networking
#
# Kubernetes does not get started by default because it might
# not always be needed in the image, depending on the test.
# _work/kube-clear-kvm can be used to start it.

SHELL=bash
RELEASE=$(shell curl -sSL https://dl.k8s.io/release/stable.txt)
KUBEADM=/opt/bin/kubeadm
_work/clear-kvm-original.img:
	mkdir -p _work && \
	cd _work && \
	dd if=/dev/random bs=1 count=8 2>/dev/null | od -A n -t x8 >passwd | sed -e 's/ //g' && \
	version=$$(curl https://download.clearlinux.org/image/ 2>&1 | grep clear-.*-kvm.img.xz | sed -e 's/.*clear-\([0-9]*\)-kvm.img.*/\1/' | sort -u -n | tail -1) && \
	[ "$$version" ] && \
	curl -O https://download.clearlinux.org/image/clear-$$version-kvm.img.xz && \
	curl -O https://download.clearlinux.org/image/clear-$$version-kvm.img.xz-SHA512SUMS && \
	curl -O https://download.clearlinux.org/image/clear-$$version-kvm.img.xz-SHA512SUMS.sig && \
	(echo 'skipping image verification, does not work at the moment (https://github.com/clearlinux/distribution/issues/85)' && true || openssl smime -verify -in clear-$$version-kvm.img.xz-SHA512SUMS.sig -inform der -content clear-$$version-kvm.img.xz-SHA512SUMS -CAfile ../test/ClearLinuxRoot.pem -out /dev/null) && \
	sha512sum -c clear-$$version-kvm.img.xz-SHA512SUMS && \
	unxz -c <clear-$$version-kvm.img.xz >clear-kvm-original.img

_work/clear-kvm.img: _work/clear-kvm-original.img _work/OVMF.fd _work/start-clear-kvm _work/ssh-clear-kvm _work/id
	set -x && \
	cp $< $@ && \
	cd _work && \
	coproc { ./start-clear-kvm clear-kvm.img | tee serial.log ;} && \
	trap '[ "$$COPROC_PID" ] && kill $$COPROC_PID' EXIT && \
	echo "Waiting for initial root login, see $$(pwd)/serial.log" && \
	while IFS= read -d : -ru $${COPROC[0]} x && ! [[ "$$x" =~ "login" ]]; do echo "XXX $$x XXX" >>/tmp/log; done && \
	echo "root" >&$${COPROC[1]} && \
	echo "Changing root password..." && \
	while IFS= read -d : -ru $${COPROC[0]} x && ! [[ "$$x" =~ "New password" ]]; do echo "YYY $$x XXX" >>/tmp/log; done && \
	echo "root$$(cat passwd)" >&$${COPROC[1]} && \
	while IFS= read -d : -ru $${COPROC[0]} x && ! [[ "$$x" =~ "Retype new password" ]]; do echo "ZZZ $$x XXX" >>/tmp/log; done && \
	echo "root$$(cat passwd)" >&$${COPROC[1]} && \
	echo "Reconfiguring and shutting down..." && \
	IFS= read -d '#' -ru $${COPROC[0]} x && \
	echo "mkdir -p /etc/ssh && echo 'PermitRootLogin yes' >> /etc/ssh/sshd_config && mkdir -p .ssh && echo '$$(cat id.pub)' >>.ssh/authorized_keys" >&$${COPROC[1]} && \
	echo "configuring Kubernetes" && \
	./ssh-clear-kvm 'swupd bundle-add cloud-native-basic cryptography' && \
	./ssh-clear-kvm 'systemctl daemon-reload && systemctl enable rngd' && \
	./ssh-clear-kvm 'ln -s /usr/share/defaults/etc/hosts /etc/hosts' && \
	./ssh-clear-kvm 'mkdir -p /etc/systemd/system/kubelet.service.d/' && \
	echo "Downloading Kubernetes $(RELEASE)." && \
	./ssh-clear-kvm	'mkdir -p /opt/bin && cd /opt/bin && for i in kubeadm kubelet kubectl; do curl -L --remote-name-all https://storage.googleapis.com/kubernetes-release/release/$(RELEASE)/bin/linux/amd64/$$i && chmod +x $$i; done' && \
	echo "Using a mixture of Clear Linux CNI plugins (/usr/libexec/cni/) and plugins downloaded via pods (/opt/cni/bin)" && \
	./ssh-clear-kvm "( echo '[Service]'; echo 'Environment=\"KUBELET_EXTRA_ARGS=--bootstrap-kubeconfig=/etc/kubernetes/bootstrap-kubelet.conf --runtime-request-timeout=30m --fail-swap-on=false --cni-bin-dir=/opt/cni/bin --allow-privileged=true --feature-gates=CSIPersistentVolume=true,MountPropagation=true\"'; echo 'ExecStart='; grep ^ExecStart= /lib/systemd/system/kubelet.service | sed -e 's;/usr/bin/kubelet;/opt/bin/kubelet;' ) >/etc/systemd/system/kubelet.service.d/clear.conf" && \
	./ssh-clear-kvm 'mkdir -p /opt/cni/bin/; for i in /usr/libexec/cni/*; do ln -s $$i /opt/cni/bin/; done' && \
	./ssh-clear-kvm 'mkdir -p /etc/systemd/system/docker.service.d/' && \
	./ssh-clear-kvm "( echo '[Service]'; echo 'ExecStart='; echo 'ExecStart=/usr/bin/dockerd --storage-driver=overlay2 --default-runtime=runc' ) >/etc/systemd/system/docker.service.d/clear.conf" && \
	./ssh-clear-kvm "mkdir -p /etc/docker && echo '{ \"insecure-registries\":[\"$$(hostname):5000\"] }' >/etc/docker/daemon.json" && \
	./ssh-clear-kvm 'systemctl daemon-reload && systemctl restart docker' && \
	./ssh-clear-kvm '$(KUBEADM) init --apiserver-cert-extra-sans localhost --kubernetes-version $(RELEASE) --ignore-preflight-errors=Swap,SystemVerification,CRI' && \
	./ssh-clear-kvm 'mkdir -p .kube' && \
	./ssh-clear-kvm 'cp -i /etc/kubernetes/admin.conf .kube/config' && \
	./ssh-clear-kvm 'kubectl taint nodes --all node-role.kubernetes.io/master-' && \
	./ssh-clear-kvm 'kubectl get pods --all-namespaces' && \
        ./ssh-clear-kvm 'kubectl apply -f https://cloud.weave.works/k8s/net?k8s-version=$$(kubectl version | base64 | tr -d "\n")' && \
	echo "Use $$(pwd)/clear-kvm-kube.config as KUBECONFIG to access the running cluster." && \
	./ssh-clear-kvm 'cat /etc/kubernetes/admin.conf' | sed -e 's;https://.*:6443;https://localhost:16443;' >clear-kvm-kube.config && \
	( echo "#!/bin/sh -e"; echo "$$(pwd)/ssh-clear-kvm 'systemctl start docker && systemctl start kubelet'"; echo 'cnt=0; while [ $$cnt -lt 10 ]; do'; echo "if $$(pwd)/ssh-clear-kvm kubectl get nodes >/dev/null; then exit 0; fi;"; echo 'cnt=$$(expr $$cnt + 1); sleep 1; done; exit 1' ) >kube-clear-kvm && chmod a+rx kube-clear-kvm && \
	./kube-clear-kvm && \
	echo "shutdown now" >&$${COPROC[1]} && wait

# This workaround was needed when using kubeadm 1.9 to set up a kubernetes 1.10 cluster,
# because of a kube-proxy config change:
#	./ssh-clear-kvm 'for i in kube-proxy kubeadm-config; do kubectl get -n kube-system -o yaml configmap $$i | sed -e "s/featureGates:.../featureGates: {}/" >/tmp/patch.yaml && kubectl patch -n kube-system -o yaml configmap $$i -p "$$(cat /tmp/patch.yaml)"; done' && \

# Ensures that (among others) _work/clear-kvm.img gets deleted when configuring it fails.
.DELETE_ON_ERROR:

_work/start-clear-kvm: test/start_qemu.sh
	mkdir -p _work
	cp $< $@
	sed -i -e "s;\(OVMF.fd\|[a-zA-Z0-9_]*\.log\);$$(pwd)/_work/\1;g" $@
	chmod a+x $@

_work/ssh-clear-kvm: _work/id
	echo "#!/bin/sh" >$@
	echo "exec ssh -p \$${VMM:-1}0022 -oIdentitiesOnly=yes -oStrictHostKeyChecking=no -oUserKnownHostsFile=/dev/null -oLogLevel=error -i $$(pwd)/_work/id root@localhost \"\$$@\"" >>$@
	chmod u+x $@

_work/OVMF.fd:
	mkdir -p _work
	curl -o $@ https://download.clearlinux.org/image/OVMF.fd

_work/id:
	mkdir -p _work
	ssh-keygen -N '' -f $@

# protobuf API handling
OIM_SPEC := spec.md
OIM_PROTO := oim.proto

# This is the target for building the temporary OIM protobuf file.
#
# The temporary file is not versioned, and thus will always be
# built on Travis-CI.
$(OIM_PROTO).tmp: $(OIM_SPEC) Makefile
	echo "// Code generated by make; DO NOT EDIT." > "$@"
	cat $< | sed -n -e '/```protobuf$$/,/^```$$/ p' | sed '/^```/d' >> "$@"

# This is the target for building the OIM protobuf file.
#
# This target depends on its temp file, which is not versioned.
# Therefore when built on Travis-CI the temp file will always
# be built and trigger this target. On Travis-CI the temp file
# is compared with the real file, and if they differ the build
# will fail.
#
# Locally the temp file is simply copied over the real file.
$(OIM_PROTO): $(OIM_PROTO).tmp
ifeq (true,$(TRAVIS))
	diff "$@" "$?"
else
	diff "$@" "$?" > /dev/null 2>&1 || cp -f "$?" "$@"
endif

# If this is not running on Travis-CI then for sake of convenience
# go ahead and update the language bindings as well.
ifneq (true,$(TRAVIS))
#build:
#	$(MAKE) -C lib/go
#	$(MAKE) -C lib/cxx
endif

#clean:
#	$(MAKE) -C lib/go $

#clobber: clean
#        $(MAKE) -C lib/go $@
#        rm -f $(OIM_PROTO) $(OIM_PROTO).tmp

# check generated files for violation of standards
test: test_proto
test_proto: $(OIM_PROTO)
	awk '{ if (length > 72) print NR, $$0 }' $? | diff - /dev/null

update: update_spec
update_spec: $(OIM_PROTO)
	$(MAKE) -C pkg/spec
