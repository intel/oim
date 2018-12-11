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
TEST_ALL=$(IMPORT_PATH)/pkg/... $(IMPORT_PATH)/test/e2e $(IMPORT_PATH)/test/pkg/...
TEST_ARGS=$(IMPORT_PATH)/pkg/... $(if $(_TEST_QEMU_IMAGE), $(IMPORT_PATH)/test/e2e)

.PHONY: test
test: all run_tests

# gometalinter (https://github.com/alecthomas/gometalinter) might not
# be installed, so we skip the test if that is the case.
LINTER = $(shell PATH=$$GOPATH/bin:$$PATH command -v gometalinter || echo true)

# Packages that don't need to pass lint checking are explicitly
# excluded.
TEST_LINT_EXCLUDE =
# Vendored code must be checked upstream.
TEST_LINT_EXCLUDE += $(IMPORT_PATH)/vendor
# Generated code.
TEST_LINT_EXCLUDE += $(IMPORT_PATH)/pkg/spec/oim/v0
# pkg/mount is temporarily copied from Kubernetes (TODO: replace with https://github.com/kubernetes/kubernetes/pull/68513 once that is merged)
TEST_LINT_EXCLUDE += $(IMPORT_PATH)/pkg/mount
# test code will soon be replaced (https://github.com/kubernetes/kubernetes/pull/70992)
TEST_LINT_EXCLUDE += $(IMPORT_PATH)/test/e2e

# TODO: Simplifying the code can come later.
LINTER += --disable=gocyclo

# These tools parse a file written for go 1.11 although the current go (or the go they were were built with?) is
# older and then fail:
# vendor/golang.org/x/net/http2/go111.go:26:16:warning: error return value not checked (invalid operation: trace (variable of type *net/http/httptrace.ClientTrace) has no field or method Got1xxResponse) (errcheck)
# TODO: try with go 1.11
LINTER += --disable=errcheck --disable=maligned --disable=structcheck --disable=varcheck --disable=interfacer --disable=unconvert --disable=megacheck --disable=gotype

# Shadowing variables "err" and "ctx" is often intentional, therefore this check led to too many false positives.
LINTER += --disable=vetshadow

.PHONY: lint test_lint
test: test_lint
test_lint:
	$(LINTER) $$(go list $(IMPORT_PATH)/... | grep -v $(foreach i,$(TEST_LINT_EXCLUDE),-e '$(i)') | sed -e 's;$(IMPORT_PATH)/;./;')

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
run_tests: $(TEST_QEMU_DEPS) $(_TEST_SPDK_VHOST_BINARY) $(TEST_E2E_DEPS) oim-csi-driver _work/ca/.ca-stamp _work/evil-ca/.ca-stamp
	TEST_OIM_CSI_DRIVER_BINARY=$(abspath _output/oim-csi-driver) \
	TEST_SPDK_VHOST_SOCKET=$(abspath $(TEST_SPDK_VHOST_SOCKET)) \
	TEST_SPDK_VHOST_BINARY=$(abspath $(_TEST_SPDK_VHOST_BINARY)) \
	TEST_QEMU_IMAGE=$(abspath $(_TEST_QEMU_IMAGE)) \
	TEST_WORK=$(abspath _work) \
	REPO_ROOT=$(abspath .) \
	KUBECONFIG=$(abspath _work)/clear-kvm-kube.config \
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
	-e 's;github.com/golang/protobuf;golang-protobuf,https://github.com/golang/protobuf;' \
	-e 's;github.com/pkg/errors;pkg/errors,https://github.com/pkg/errors;' \
	-e 's;github.com/vgough/grpc-proxy;grpc-proxy,https://github.com/vgough/grpc-proxy;' \
	-e 's;golang.org/x/.*;Go,https://github.com/golang/go;' \
	-e 's;k8s.io/.*;kubernetes,https://github.com/kubernetes/kubernetes;' \
	-e 's;github.com/kubernetes-csi/.*;kubernetes,https://github.com/kubernetes/kubernetes;' \
	-e 's;gopkg.in/fsnotify.*;golang-github-fsnotify-fsnotify,https://github.com/fsnotify/fsnotify;' \
	| cat |

# Ignore duplicates.
RUNTIME_DEPS += sort -u

_work/%/.ca-stamp: test/setup-ca.sh
	rm -rf $(@D)
	NUM_NODES=$(NUM_NODES) DEPOT_PATH='$(@D)' CA='$*' $<
	touch $@

include test/clear-kvm.make
include test/start-stop.make
