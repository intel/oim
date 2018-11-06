all: build

########################################################################
##                             GOLANG                                 ##
########################################################################

# If GOPATH isn't defined then set its default location.
ifeq (,$(strip $(GOPATH)))
GOPATH := $(HOME)/go
else
# If GOPATH is already set then update GOPATH to be its own
# first element.
GOPATH := $(word 1,$(subst :, ,$(GOPATH)))
endif
export GOPATH


########################################################################
##                             PROTOC                                 ##
########################################################################

# Only set PROTOC_VER if it has an empty value.
ifeq (,$(strip $(PROTOC_VER)))
PROTOC_VER := 3.5.1
endif

PROTOC_OS := $(shell uname -s)
ifeq (Darwin,$(PROTOC_OS))
PROTOC_OS := osx
endif

PROTOC_ARCH := $(shell uname -m)
ifeq (i386,$(PROTOC_ARCH))
PROTOC_ARCH := x86_32
endif

PROTOC := ./protoc
PROTOC_ZIP := protoc-$(PROTOC_VER)-$(PROTOC_OS)-$(PROTOC_ARCH).zip
PROTOC_URL := https://github.com/google/protobuf/releases/download/v$(PROTOC_VER)/$(PROTOC_ZIP)
PROTOC_TMP_DIR := .protoc
PROTOC_TMP_BIN := $(PROTOC_TMP_DIR)/bin/protoc

$(PROTOC):
	-mkdir -p "$(PROTOC_TMP_DIR)" && \
	  curl -L $(PROTOC_URL) -o "$(PROTOC_TMP_DIR)/$(PROTOC_ZIP)" && \
	  unzip "$(PROTOC_TMP_DIR)/$(PROTOC_ZIP)" -d "$(PROTOC_TMP_DIR)" && \
	  chmod 0755 "$(PROTOC_TMP_BIN)" && \
	  cp -f "$(PROTOC_TMP_BIN)" "$@"
	stat "$@" > /dev/null 2>&1


########################################################################
##                          PROTOC-GEN-GO                             ##
########################################################################

# This is the recipe for getting and installing the go plug-in
# for protoc
PROTOC_GEN_GO_BINARY := gogofaster
PROTOC_GEN_GO := protoc-gen-$(PROTOC_GEN_GO_BINARY)
PROTOC_GEN_GO_PKG := github.com/gogo/protobuf/$(PROTOC_GEN_GO)
$(PROTOC_GEN_GO): PROTOBUF_PKG := $(dir $(PROTOC_GEN_GO_PKG))
$(PROTOC_GEN_GO): PROTOBUF_VERSION := v1.0.0
$(PROTOC_GEN_GO):
	mkdir -p $(dir $(GOPATH)/src/$(PROTOBUF_PKG))
	test -d $(GOPATH)/src/$(PROTOBUF_PKG)/.git || git clone https://$(PROTOBUF_PKG) $(GOPATH)/src/$(PROTOBUF_PKG)
	(cd $(GOPATH)/src/$(PROTOBUF_PKG) && \
		(test "$$(git describe --tags | head -1)" = "$(PROTOBUF_VERSION)" || \
			(git fetch && git checkout tags/$(PROTOBUF_VERSION))))
	(cd $(GOPATH)/src/$(PROTOBUF_PKG) && go get -v -d ./...) && \
	go build -o "$@" $(PROTOC_GEN_GO_PKG)


########################################################################
##                              PATH                                  ##
########################################################################

# Update PATH with the current directory. This enables the protoc
# binary to discover the protoc-gen-go binary, built inside this
# directory.
export PATH := $(shell pwd):$(PATH)


########################################################################
##                              BUILD                                 ##
########################################################################
GEN_PBGO :=

GEN_PBGO += OIM
OIM_PROTO := oim.proto
OIM_PKG := $(shell cat $(OIM_PROTO) | sed -n -e 's/^package.\([^;]*\);$$/\1/p'|tr '.' '/')
OIM_GO := $(OIM_PKG)/oim.pb.go
OIM_GO_TMP := .build/oim.pb.go

# This recipe generates the go language bindings to a temp area.
GO_OUT := plugins=grpc
GO_OUT := $(GO_OUT),Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types
GO_OUT := $(GO_OUT),Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types
GO_OUT := $(GO_OUT),Mgoogle/protobuf/duration.proto=github.com/gogo/protobuf/types
GO_OUT := $(GO_OUT),Mgoogle/protobuf/struct.proto=github.com/gogo/protobuf/types
GO_OUT := $(GO_OUT),Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types
GO_OUT := $(GO_OUT),Mgoogle/protobuf/wrappers.proto=github.com/gogo/protobuf/types
INCLUDE = -I$(PROTOC_TMP_DIR)/include -I../../vendor/github.com/gogo/protobuf
$(foreach i,$(GEN_PBGO),$($(i)_GO_TMP)): .build/%.pb.go: %.proto | $(PROTOC) $(PROTOC_GEN_GO)
	@mkdir -p "$(@D)"
	$(PROTOC) -I "$(<D)" $(INCLUDE) --$(PROTOC_GEN_GO_BINARY)_out=$(GO_OUT):"$(shell echo $@ | sed -e 's;\(.build\(/vendor\)*\)/.*;\1;')" "$<"
	: https://github.com/golang/protobuf/issues/537
	sed -i -e 's;context "golang.org/x/net/context";"context";' "$@"

# The temp language bindings are compared to the ones that are
# versioned. If they are different then it means the language
# bindings were not updated prior to being committed.
ifeq (true,$(TRAVIS))
GO_FROM_TMP = diff "$(1)" "$(2)"
else
GO_FROM_TMP = (mkdir -p "$(dir $(2))" && cmp "$(1)" "$(2)" > /dev/null 2>&1 && echo "$(2) up-to-date" || echo "updating $(2)" && cp -f "$(1)" "$(2)")
endif
build: $(foreach i,$(GEN_PBGO),$($(i)_GO_TMP))
	@ $(foreach i,$(GEN_PBGO),$(call GO_FROM_TMP,$($(i)_GO_TMP),$($(i)_GO)) &&) true

clean:
	go clean -i ./...
	rm -rf .build

clobber: clean
	rm -fr "$(PROTOC)" "$(PROTOC_GEN_GO)" "$(OIM_PKG)" .protoc

.PHONY: clean clobber
