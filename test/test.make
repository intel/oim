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
	mkdir -p _work
	cd _work && \
	TEST_OIM_CSI_DRIVER_BINARY=$(abspath _output/oim-csi-driver) \
	TEST_SPDK_VHOST_SOCKET=$(abspath $(TEST_SPDK_VHOST_SOCKET)) \
	TEST_SPDK_VHOST_BINARY=$(abspath $(_TEST_SPDK_VHOST_BINARY)) \
	TEST_QEMU_IMAGE=$(abspath $(_TEST_QEMU_IMAGE)) \
	    $(TEST_CMD) $$( go list $(TEST_ARGS) | sed -e 's;$(IMPORT_PATH);../;' )

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

# Hostname set inside the virtual machine.
GUEST_HOSTNAME = kubernetes-master

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

_work/clear-kvm.img: _work/clear-kvm-original.img _work/kube-clear-kvm _work/OVMF.fd _work/start-clear-kvm _work/ssh-clear-kvm _work/id
	cp $< $@
	$(SETUP_CLEAR_IMG)

SETUP_CLEAR_IMG = true
# We run with tracing enabled, but suppress the trace output for some
# parts with 2>/dev/null (in particular, echo commands) to keep the
# output a bit more readable.
SETUP_CLEAR_IMG += && set -x
SETUP_CLEAR_IMG += && cd _work
# coproc runs the shell commands in a separate process, with
# stdin/stdout available to the caller.
SETUP_CLEAR_IMG += && coproc sh -c './start-clear-kvm clear-kvm.img | tee serial.log'
# bash will detect when the coprocess dies and then unset the COPROC variables.
# We can use that to check that QEMU is still healthy and avoid "ambiguous redirect"
# errors when reading or writing to empty variables.
SETUP_CLEAR_IMG += && qemu_running () { ( if ! [ "$$COPROC_PID" ]; then echo "ERRROR: QEMU died unexpectedly, see error messages above."; false; fi ) 2>/dev/null; }
# Wait for certain output from the co-process.
SETUP_CLEAR_IMG += && waitfor () { ( term="$$1"; while IFS= read -d : -r x && ! [[ "$$x" =~ "$$term" ]]; do :; done ) 2>/dev/null; }
# We know the PID of the bash process running the pipe, but what we
# really need to kill is the QEMU child process.
SETUP_CLEAR_IMG += && trap '( [ "$$COPROC_PID" ] && echo "killing co-process and children with PID $$COPROC_PID"; kill $$(ps -o pid --ppid $$COPROC_PID | tail +2) $$COPROC_PID ) 2>/dev/null' EXIT
SETUP_CLEAR_IMG += && ( echo "Waiting for initial root login, see $$(pwd)/serial.log" ) 2>/dev/null
SETUP_CLEAR_IMG += && qemu_running && waitfor "login" <&$${COPROC[0]}
# We get some extra messages on the console that should be printed
# before we start interacting with the console prompt.
SETUP_CLEAR_IMG += && ( echo "Give Clear Linux some time to finish booting." ) 2>/dev/null
SETUP_CLEAR_IMG += && sleep 5
SETUP_CLEAR_IMG += && ( echo "Changing root password..." ) 2>/dev/null
SETUP_CLEAR_IMG += && qemu_running && echo "root" >&$${COPROC[1]}
SETUP_CLEAR_IMG += && qemu_running && waitfor "New password" <&$${COPROC[0]}
SETUP_CLEAR_IMG += && qemu_running && echo "$$(cat passwd)" >&$${COPROC[1]}
SETUP_CLEAR_IMG += && qemu_running && waitfor "Retype new password" <&$${COPROC[0]}
SETUP_CLEAR_IMG += && qemu_running && echo "$$(cat passwd)" >&$${COPROC[1]}
SETUP_CLEAR_IMG += && ( echo "Reconfiguring..." ) 2>/dev/null
SETUP_CLEAR_IMG += && qemu_running && echo "mkdir -p /etc/ssh && echo 'PermitRootLogin yes' >> /etc/ssh/sshd_config && mkdir -p .ssh && echo '$$(cat id.pub)' >>.ssh/authorized_keys" >&$${COPROC[1]}

# Set up the static network configuration, both for booting with and without network interface renaming.
SETUP_CLEAR_IMG += && qemu_running && echo "mkdir -p /etc/systemd/network" >&$${COPROC[1]}
SETUP_CLEAR_IMG += && qemu_running && for i in "[Match]" "Name=ens4" "[Network]" "Address=192.168.7.2/24" "Gateway=192.168.7.1" "DNS=8.8.8.8"; do echo "echo '$$i' >>/etc/systemd/network/20-wired.network" >&$${COPROC[1]}; done
SETUP_CLEAR_IMG += && qemu_running && for i in "[Match]" "Name=eth0" "[Network]" "Address=192.168.7.2/24" "Gateway=192.168.7.1" "DNS=8.8.8.8"; do echo "echo '$$i' >>/etc/systemd/network/20-wired.network" >&$${COPROC[1]}; done
SETUP_CLEAR_IMG += && qemu_running && echo "systemctl restart systemd-networkd" >&$${COPROC[1]}

SETUP_CLEAR_IMG += && ( echo "Configuring Kubernetes..." ) 2>/dev/null
# Install kubelet, kubeadm and CRI-O.
SETUP_CLEAR_IMG += && ./ssh-clear-kvm "$(PROXY_ENV) swupd bundle-add cloud-native-basic"
# Enable IP Forwarding.
SETUP_CLEAR_IMG += && ./ssh-clear-kvm 'mkdir /etc/sysctl.d && echo net.ipv4.ip_forward = 1 >/etc/sysctl.d/60-k8s.conf && systemctl restart systemd-sysctl'
# Due to stateless /etc is empty but /etc/hosts is needed by k8s pods.
# It also expects that the local host name can be resolved. Let's use a nicer one
# instead of the normal default (clear-<long hex string>).
SETUP_CLEAR_IMG += && ./ssh-clear-kvm 'hostnamectl set-hostname $(GUEST_HOSTNAME)' && ./ssh-clear-kvm 'echo 127.0.0.1 localhost $(GUEST_HOSTNAME) >>/etc/hosts'
# br_netfilter must be loaded explicitly on the Clear Linux KVM kernel (and only there),
# otherwise the required /proc/sys/net/bridge/bridge-nf-call-iptables isn't there.
SETUP_CLEAR_IMG += && ./ssh-clear-kvm modprobe br_netfilter && ./ssh-clear-kvm 'echo br_netfilter >>/etc/modules'
# Disable swap (permanently).
SETUP_CLEAR_IMG += && ./ssh-clear-kvm systemctl mask $$(./ssh-clear-kvm cat /proc/swaps | sed -n -e 's;^/dev/\([0-9a-z]*\).*;dev-\1.swap;p')
SETUP_CLEAR_IMG += && ./ssh-clear-kvm swapoff -a
# proxy settings for CRI-O
SETUP_CLEAR_IMG += && ./ssh-clear-kvm mkdir /etc/systemd/system/crio.service.d
SETUP_CLEAR_IMG += && ./ssh-clear-kvm "( echo '[Service]'; echo 'Environment=\"HTTP_PROXY=$(HTTP_PROXY)\" \"HTTPS_PROXY=$(HTTPS_PROXY)\" \"NO_PROXY=$(NO_PROXY)\"') >/etc/systemd/system/crio.service.d/proxy.conf"
# Testing may involve a Docker registry running on the build host (see
# REGISTRY_NAME). We need to trust that registry, otherwise CRI-O
# will fail to pull images from it. insecure-registries is commented out,
# so we can simply replace that entire line.
# Creating an entirely separate /etc/crio/crio.conf isn't ideal because
# future updates to /usr/share/defaults/crio/crio.conf
SETUP_CLEAR_IMG += && ./ssh-clear-kvm mkdir -p /etc/crio
SETUP_CLEAR_IMG += && ./ssh-clear-kvm cat /usr/share/defaults/crio/crio.conf | sed -e  "s^.*insecure_registries.*=.*.*^insecure_registries = [ '192.168.7.1:5000' ]^" | ./ssh-clear-kvm "cat >/etc/crio/crio.conf"
# Tell CRI-O to use the simple lookback networking.
# However, for this to work we have to have "eth0" around.
# If we allow systemd to rename the interface, some command is run
# against eth0, which causes errors during pod creation:
# Sep 19 13:39:57 kubernetes-master kubelet[424]: E0919 13:39:57.508001     424 kuberuntime_sandbox.go:56] CreatePodSandbox for pod "coredns-78fcdf6894-5jjw6_kube-system(0a72f529-bc11-11e8-90ed-525400123456)" failed: rpc error: code = Unknown desc = failed to get network status for pod sandbox k8s_coredns-78fcdf6894-5jjw6_kube-system_0a72f529-bc11-11e8-90ed-525400123456_0(7d913cfcc8282e0e1ddd152207cf650d7977524ada42d3559f3e93bb76b39013): Unexpected command output Device "eth0" does not exist.
SETUP_CLEAR_IMG += && ./ssh-clear-kvm mkdir -p /etc/cni/net.d && echo '{ "type": "loopback" }' |  ./ssh-clear-kvm 'cat >/etc/cni/net.d/99-loopback.conf'
SETUP_CLEAR_IMG += && ./ssh-clear-kvm mkdir -p /etc/systemd/network/ && ./ssh-clear-kvm ln -s /dev/null /etc/systemd/network/99-default.link
# Reconfiguration done, start daemons.
SETUP_CLEAR_IMG += && ./ssh-clear-kvm 'systemctl daemon-reload && systemctl restart cri-o kubelet && systemctl enable cri-o kubelet'
# We allow API access also via 192.168.7.2, because that's what we are going to
# use below for connecting directly from the host.
SETUP_CLEAR_IMG += && ./ssh-clear-kvm '$(PROXY_ENV) kubeadm init --apiserver-cert-extra-sans 192.168.7.2 --cri-socket /var/run/crio/crio.sock'
SETUP_CLEAR_IMG += && ./ssh-clear-kvm 'mkdir -p .kube'
SETUP_CLEAR_IMG += && ./ssh-clear-kvm 'cp -i /etc/kubernetes/admin.conf .kube/config'
SETUP_CLEAR_IMG += && ./ssh-clear-kvm 'kubectl taint nodes --all node-role.kubernetes.io/master-'
# Done.
SETUP_CLEAR_IMG += && ( echo "Use $$(pwd)/clear-kvm-kube.config as KUBECONFIG to access the running cluster." ) 2>/dev/null
SETUP_CLEAR_IMG += && ./ssh-clear-kvm 'cat /etc/kubernetes/admin.conf' | sed -e 's;https://.*:6443;https://192.168.7.2:6443;' >clear-kvm-kube.config
# Verify that Kubernetes works by starting it and then listing pods.
# We also wait for the node to become ready, which can take a while because
# images might still need to be pulled. This can take minutes, therefore we sleep
# for one minute between output.
SETUP_CLEAR_IMG += && ( echo "Waiting for Kubernetes cluster to become ready..." ) 2>/dev/null
SETUP_CLEAR_IMG += && ./kube-clear-kvm
SETUP_CLEAR_IMG += && while ! ./ssh-clear-kvm kubectl get nodes | grep -q '$(GUEST_HOSTNAME) *Ready'; do sleep 60; ./ssh-clear-kvm kubectl get nodes; ./ssh-clear-kvm kubectl get pods --all-namespaces; done
SETUP_CLEAR_IMG += && ./ssh-clear-kvm kubectl get nodes; ./ssh-clear-kvm kubectl get pods --all-namespaces
# Doing the same locally only works if we have kubectl
SETUP_CLEAR_IMG += && if command -v kubectl >/dev/null; then kubectl --kubeconfig ./clear-kvm-kube.config get pods --all-namespaces; fi
SETUP_CLEAR_IMG += && qemu_running && echo "shutdown now" >&$${COPROC[1]} && wait

_work/start-clear-kvm: test/start_qemu.sh
	mkdir -p _work
	cp $< $@
	sed -i -e "s;\(OVMF.fd\|[a-zA-Z0-9_]*\.log\);$$(pwd)/_work/\1;g" $@
	chmod a+x $@

_work/kube-clear-kvm: test/start_kubernetes.sh _work/ssh-clear-kvm
	mkdir -p _work
	cp $< $@
	sed -i -e "s;SSH;$$(pwd)/_work/ssh-clear-kvm;g" $@
	chmod u+x $@

_work/ssh-clear-kvm: _work/id
	echo "#!/bin/sh" >$@
	echo "exec ssh -oIdentitiesOnly=yes -oStrictHostKeyChecking=no -oUserKnownHostsFile=/dev/null -oLogLevel=error -i $$(pwd)/_work/id root@192.168.7.2 \"\$$@\"" >>$@
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
