/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimregistry

import (
	"sync"
)

// MemRegistryDB implements an in-memory DB for Registry. Each call is
// protected against concurrent access via locking.
type MemRegistryDB struct {
	db    map[string]string
	mutex sync.Mutex
}

func NewMemRegistryDB() *MemRegistryDB {
	m := &MemRegistryDB{}
	m.db = make(map[string]string)
	return m
}

func (m *MemRegistryDB) Store(controllerID, address string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if address == "" {
		delete(m.db, controllerID)
	} else {
		m.db[controllerID] = address
	}
}
func (m *MemRegistryDB) Lookup(controllerID string) (address string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return m.db[controllerID]
}
func (m *MemRegistryDB) Foreach(callback func(controllerID, address string) bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for controllerID, address := range m.db {
		if !callback(controllerID, address) {
			return
		}
	}
}
