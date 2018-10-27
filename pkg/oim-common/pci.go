/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/pkg/errors"

	"github.com/intel/oim/pkg/spec/oim/v0"
)

var bdfRe = regexp.MustCompile(`^\s*(?:([0-9a-fA-F]{0,4}):)?([0-9a-fA-F]{0,2}):([0-9a-fA-F]{0,2})\.([0-7]{0,1})\s*$`)

// HexToI32 takes 0 to 4 hex digits and turns them into an uint32. It
// panics on invalid content, so the caller must check for valid input
// in advance. 0xFFFF is the default if the string is empty.
func HexToU32(hex string) uint32 {
	if hex == "" {
		return 0xFFFF
	}
	value, err := strconv.ParseUint(hex, 16, 16)
	if err != nil {
		panic(err)
	}
	return uint32(value)
}

// ParseBSDString accepts a PCI address in extended BDF notation.
func ParseBDFString(dev string) (*oim.PCIAddress, error) {
	parts := bdfRe.FindStringSubmatch(dev)
	if len(parts) == 0 {
		return nil, errors.Errorf("%q not in BDF notation ([[domain]:][bus]:[dev].[function])", dev)
	}
	return &oim.PCIAddress{
		Domain:   HexToU32(parts[1]),
		Bus:      HexToU32(parts[2]),
		Device:   HexToU32(parts[3]),
		Function: HexToU32(parts[4]),
	}, nil
}

// CompletePCIAddress merges two PCI addresses, filling in unknown fields from
// the default.
func CompletePCIAddress(addr, def oim.PCIAddress) oim.PCIAddress {
	if addr.Domain == 0xFFFF {
		addr.Domain = def.Domain
	}
	if addr.Bus == 0xFFFF {
		addr.Bus = def.Bus
	}
	if addr.Device == 0xFFFF {
		addr.Device = def.Device
	}
	if addr.Function == 0xFFFF {
		addr.Function = def.Function
	}
	return addr
}

// PrettyPCIAddress formats a PCI address in extended BDF format.
func PrettyPCIAddress(p *oim.PCIAddress) string {
	if p == nil {
		return ":."
	}
	var result string
	if p.Domain != 0xFFFF {
		result += fmt.Sprintf("%04x:", p.Domain)
	}
	if p.Bus != 0xFFFF {
		result += fmt.Sprintf("%02x:", p.Bus)
	} else {
		result += ":"
	}
	if p.Device != 0xFFFF {
		result += fmt.Sprintf("%02x.", p.Device)
	} else {
		result += "."
	}
	if p.Function != 0xFFFF {
		result += fmt.Sprintf("%x", p.Function)
	}
	return result
}
