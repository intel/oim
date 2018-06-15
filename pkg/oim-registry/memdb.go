/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimregistry

// MemRegistryDB implements an in-memory DB for Registry.
type MemRegistryDB map[string]string

func (m MemRegistryDB) Store(controllerID, address string) {
	if address == "" {
		delete(m, controllerID)
	} else {
		m[controllerID] = address
	}
}
func (m MemRegistryDB) Lookup(controllerID string) (address string) {
	return m[controllerID]
}
