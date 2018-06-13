/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimregistry

// MemRegistryDB implements an in-memory DB for Registry.
type MemRegistryDB map[string]string

func (m MemRegistryDB) Store(hardwareID, address string) {
	if address == "" {
		delete(m, hardwareID)
	} else {
		m[hardwareID] = address
	}
}
func (m MemRegistryDB) Lookup(hardwareID string) (address string) {
	return m[hardwareID]
}
