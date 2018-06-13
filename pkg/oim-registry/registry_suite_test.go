/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimregistry_test

import (
	"log"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func init() {
	log.SetOutput(GinkgoWriter)
}

func TestOIMRegistry(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OIM Registry Suite")
}
