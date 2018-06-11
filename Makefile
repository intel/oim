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

all: oim-csi-driver

# By default, testing only runs tests that work without additional components.
# Additional tests can be enabled by overriding the following makefile variables
# or (when invoking go test manually) by setting the corresponding env variables

# Unix domain socket path of a running SPDK vhost.
TEST_SPDK_VHOST_SOCKET=

# Image base name to boot under QEMU before running tests, for example
# "clear-kvm".
TEST_QEMU_IMAGE=

TEST_CMD=go test -v
TEST_ARGS=$(IMPORT_PATH)/pkg/...

test: all $(patsubst %, _work/%.img, $(TEST_QEMU_IMAGE)) $(patsubst %, _work/start-%, $(TEST_QEMU_IMAGE)) $(patsubst %, _work/ssh-%, $(TEST_QEMU_IMAGE))
	go vet $(IMPORT_PATH)/pkg/...
	mkdir -p _work
	cd _work && \
	TEST_SPDK_VHOST_SOCKET=$(TEST_SPDK_VHOST_SOCKET) \
	TEST_QEMU_IMAGE=$(addprefix $$(pwd)/, $(TEST_QEMU_IMAGE)) \
	    $(TEST_CMD) $(TEST_ARGS)

coverage:
	mkdir -p _work
	go test -coverprofile _work/cover.out $(IMPORT_PATH)/pkg/...
	go tool cover -html=_work/cover.out -o _work/cover.html

oim-csi-driver:
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o _output/$@ ./cmd/$@

# _output is used as the build context. All files inside it are sent
# to the Docker daemon when building images.
%-container: %
	cp cmd/$*/Dockerfile _output/Dockerfile.$*
	cd _output && docker build -t $(IMAGE_TAG) -f Dockerfile.$* .

push-%: %-container
	docker push $(IMAGE_TAG)

clean:
	go clean -r -x
	-rm -rf _output _work

.PHONY: all test coverage clean oim-csi-driver

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
SHELL=bash
_work/clear-kvm-original.img:
	mkdir -p _work && \
	cd _work && \
	dd if=/dev/random bs=1 count=8 2>/dev/null | od -A n -t x8 >passwd | sed -e 's/ //g' && \
	version=$$(curl https://download.clearlinux.org/image/ 2>&1 | grep clear-.*-kvm.img.xz | sed -e 's/.*clear-\([0-9]*\)-kvm.img.*/\1/' | sort -u -n | tail -1) && \
	[ "$$version" ] && \
	set -x && \
	curl -O https://download.clearlinux.org/image/clear-$$version-kvm.img.xz && \
	curl -O https://download.clearlinux.org/image/clear-$$version-kvm.img.xz-SHA512SUMS && \
	curl -O https://download.clearlinux.org/image/clear-$$version-kvm.img.xz-SHA512SUMS.sig && \
	(echo 'skipping image verification, does not work at the moment (https://github.com/clearlinux/distribution/issues/85)' && true || openssl smime -verify -in clear-$$version-kvm.img.xz-SHA512SUMS.sig -inform der -content clear-$$version-kvm.img.xz-SHA512SUMS -CAfile ../test/ClearLinuxRoot.pem -out /dev/null) && \
	sha512sum -c clear-$$version-kvm.img.xz-SHA512SUMS && \
	unxz -c <clear-$$version-kvm.img.xz >clear-kvm-original.img

_work/clear-kvm.img: _work/clear-kvm-original.img _work/OVMF.fd _work/start-clear-kvm _work/id
	cp $< $@ && \
	cd _work && \
	coproc { ./start-clear-kvm clear-kvm.img | tee serial.log ;} && \
	set +x && \
	echo "Waiting for initial root login, see _work/serial.log..." && \
	while IFS= read -d : -ru $${COPROC[0]} x && ! [[ "$$x" =~ "login" ]]; do echo "XXX $$x XXX" >>/tmp/log; done && \
	echo "root" >&$${COPROC[1]} && \
	echo "Changing root password..." && \
	while IFS= read -d : -ru $${COPROC[0]} x && ! [[ "$$x" =~ "New password" ]]; do echo "YYY $$x XXX" >>/tmp/log; done && \
	echo "root$$(cat passwd)" >&$${COPROC[1]} && \
	while IFS= read -d : -ru $${COPROC[0]} x && ! [[ "$$x" =~ "Retype new password" ]]; do echo "ZZZ $$x XXX" >>/tmp/log; done && \
	echo "root$$(cat passwd)" >&$${COPROC[1]} && \
	echo "Reconfiguring and shutting down..." && \
	IFS= read -d '#' -ru $${COPROC[0]} x && \
	echo "mkdir -p /etc/ssh && echo 'PermitRootLogin yes' >> /etc/ssh/sshd_config && mkdir -p .ssh && echo '$$(cat id.pub)' >>.ssh/authorized_keys && shutdown now" >&$${COPROC[1]} && \
	wait

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
