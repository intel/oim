/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package ginkgo_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/log/testlog"
)

func TestGinkgo(t *testing.T) {
	defer testlog.SetGlobal(GinkgoWriter)()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ginkgo Suite")
}

var _ = SynchronizedBeforeSuite(func() []byte {
	log.L().Debug("in master setup")
	return nil
}, func(data []byte) {
	log.L().Debug("in all-node setup")
})
