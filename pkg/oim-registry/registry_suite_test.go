/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimregistry_test

import (
	"testing"

	"github.com/intel/oim/pkg/log/testlog"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func init() {
	testlog.SetGlobal(GinkgoWriter)
}

func TestOIMRegistry(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OIM Registry Suite")
}
