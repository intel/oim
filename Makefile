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

all: oim-csi-driver-container

# By default, testing only runs tests that work without additional components.
# Additional tests can be enabled by overriding the following makefile variables
# or (when invoking go test manually) by setting the corresponding env variables

# Unix domain socket path of a running SPDK vhost.
TEST_SPDK_VHOST_SOCKET=

TEST_CMD=go test -v
TEST_ARGS=$(IMPORT_PATH)/pkg/...

test: $(patsubst %, _work/%.img, $(TEST_QEMU_IMAGE)) $(patsubst %, _work/start-%, $(TEST_QEMU_IMAGE)) $(patsubst %, _work/ssh-%, $(TEST_QEMU_IMAGE))
	go vet $(IMPORT_PATH)/pkg/...
	mkdir -p _work
	cd _work && \
	TEST_SPDK_VHOST_SOCKET=$(TEST_SPDK_VHOST_SOCKET) \
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
