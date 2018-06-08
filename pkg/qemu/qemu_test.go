/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package qemu

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQEMU(t *testing.T) {
	image := os.Getenv("TEST_QEMU_IMAGE")
	if image == "" {
		t.Skip("No QEMU configured via TEST_QEMU_IMAGE")
	}
	var err error

	vm, err := StartQEMU()
	require.NoError(t, err)

	var out string
	out, err = vm.SSH("echo", "hello world")
	if assert.NoError(t, err) {
		assert.Equal(t, "hello world\n", out)
	}

	script := bytes.NewBufferString(`#!/bin/sh
echo 'hello | world'
`)
	path := "/tmp/echo.sh"
	err = vm.Install(path, script, 0777)
	if assert.NoError(t, err) {
		out, err = vm.SSH(path)
		if assert.NoError(t, err) {
			assert.Equal(t, "hello | world\n", out)
		}
	}

	err = vm.StopQEMU()
	require.NoError(t, err)
}
