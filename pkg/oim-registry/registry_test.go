/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimregistry_test

import (
	"context"

	"github.com/intel/oim/pkg/oim-registry"
	"github.com/intel/oim/pkg/spec/oim/v0"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("OIM Registry", func() {
	ctx := context.Background()

	Describe("storing mapping", func() {
		It("should work", func() {
			db := oimregistry.MemRegistryDB{}
			var err error
			r, err := oimregistry.New(oimregistry.DB(db))
			Expect(err).NotTo(HaveOccurred())
			hardwareID := "foo"
			address := "tpc:///1.1.1.1/"
			_, err = r.RegisterController(ctx, &oim.RegisterControllerRequest{
				UUID:    hardwareID,
				Address: address,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(db).To(Equal(oimregistry.MemRegistryDB{hardwareID: address}))
		})
	})
})
