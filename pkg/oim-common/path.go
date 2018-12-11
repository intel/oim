/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"strings"

	"github.com/pkg/errors"
)

const (
	// RegistryAddress is the special registry path element for the gRPC target value.
	RegistryAddress = "address"

	// RegistryPCI is the special registry path element with the PCI address of an accelerator card.
	RegistryPCI = "pci"
)

// SplitRegistryPath separates the path into elements.
// It returns an error for invalid paths.
func SplitRegistryPath(path string) ([]string, error) {
	elements := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	for _, element := range elements {
		if element == "." || element == ".." {
			return nil, errors.Errorf("%s: %q not allowed as path element", path, element)
		}
	}
	return elements, nil
}

// JoinRegistryPath combines different path elements into a full path.
func JoinRegistryPath(elements []string) string {
	return strings.Join(elements, "/")
}
