/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/intel/oim/pkg/spec/oim/v0"
)

func TestHexToU32(t *testing.T) {
	cases := []struct {
		hex    string
		result uint32
		panics bool
	}{
		{"1", 1, false},
		{"1f", 0x1f, false},
		{"0ABC", 0xabc, false},
		{"11111", 0, true},
		{"", 0xFFFF, false},
	}

	for _, c := range cases {
		if c.panics {
			assert.Panics(t, func() {
				HexToU32(c.hex)
			}, "HexToU32 should have panicked for: %s", c.hex)
		} else {
			var result uint32
			assert.NotPanics(t, func() {
				result = HexToU32(c.hex)
			}, "HexToU32 should not have panicked for: %s", c.hex)
			assert.Equal(t, c.result, result)
		}
	}
}

func TestParseBDFString(t *testing.T) {
	cases := []struct {
		dev                           string
		domain, bus, device, function uint32
		err                           bool
	}{
		{"::.", 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, false},
		{":1:.", 0xFFFF, 0xFFFF, 1, 0xFFFF, false},
		{"::.7", 0xFFFF, 0xFFFF, 0xFFFF, 7, false},
		{"1:2:3.4", 1, 2, 3, 4, false},
		{"2:3.4", 0xFFFF, 2, 3, 4, false},
		{"-1", 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, true},
		{"::.8", 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, true},
		{"::.123", 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, true},
		{"::123.", 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, true},
		{":123:.", 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, true},
		{"12345::.", 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, true},
	}

	for _, c := range cases {
		res, err := ParseBDFString(c.dev)
		if c.err {
			assert.Error(t, err, "expected error for: %s", c.dev)
		} else {
			if assert.NoError(t, err, "expected no error for: %s", c.dev) &&
				assert.NotNil(t, res) {
				assert.Equal(t, *res, oim.PCIAddress{
					Domain:   res.Domain,
					Bus:      res.Bus,
					Device:   res.Device,
					Function: res.Function,
				}, "test case: %s", c.dev)
			}
		}
	}
}

func TestCompletePCIAddress(t *testing.T) {
	cases := []struct {
		base, def, result oim.PCIAddress
	}{
		{oim.PCIAddress{Domain: 0xFFFF, Bus: 0xFFFF, Device: 0xFFFF, Function: 0xFFFF}, oim.PCIAddress{Domain: 1, Bus: 2, Device: 3, Function: 4}, oim.PCIAddress{Domain: 1, Bus: 2, Device: 3, Function: 4}},
		{oim.PCIAddress{Domain: 1, Bus: 2, Device: 3, Function: 4}, oim.PCIAddress{Domain: 0xFFFF, Bus: 0xFFFF, Device: 0xFFFF, Function: 0xFFFF}, oim.PCIAddress{Domain: 1, Bus: 2, Device: 3, Function: 4}},
	}

	for _, c := range cases {
		assert.Equal(t, c.result, CompletePCIAddress(c.base, c.def))
	}
}

func TestPrettyPCIAddress(t *testing.T) {
	cases := []struct {
		address *oim.PCIAddress
		result  string
	}{
		{&oim.PCIAddress{Domain: 0xFFFF, Bus: 0xFFFF, Device: 0xFFFF, Function: 0xFFFF}, ":."},
		{&oim.PCIAddress{Domain: 1, Bus: 2, Device: 3, Function: 4}, "0001:02:03.4"},
		{&oim.PCIAddress{Domain: 0xFFFF, Bus: 2, Device: 3, Function: 4}, "02:03.4"},
		{&oim.PCIAddress{Domain: 1, Bus: 0xFFFF, Device: 3, Function: 4}, "0001::03.4"},
		{&oim.PCIAddress{Domain: 1, Bus: 2, Device: 0xFFFF, Function: 4}, "0001:02:.4"},
		{&oim.PCIAddress{Domain: 1, Bus: 2, Device: 3, Function: 0xFFFF}, "0001:02:03."},
		{&oim.PCIAddress{Domain: 0xFFFF, Bus: 0xFFFF, Device: 3, Function: 4}, ":03.4"},
		{nil, ":."},
	}

	for _, c := range cases {
		assert.Equal(t, c.result, PrettyPCIAddress(c.address))
		res, err := ParseBDFString(c.result)
		if assert.NoError(t, err) {
			if c.address != nil {
				assert.Equal(t, c.address, res)
			}
		}
	}
}
