/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package qemu_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testqemu "github.com/intel/oim/test/pkg/qemu"
)

func TestQEMU(t *testing.T) {
	var err error

	err = testqemu.Init(t)
	defer testqemu.Finalize()
	require.NoError(t, err)
	if testqemu.VM == nil {
		t.Skip("A QEMU image is required for this test.")
	}

	var out string
	out, err = testqemu.VM.SSH("echo", "hello world")
	if assert.NoError(t, err) {
		assert.Equal(t, "hello world\n", out)
	}

	script := bytes.NewBufferString(`#!/bin/sh
echo 'hello | world'
`)
	path := "/tmp/echo.sh"
	err = testqemu.VM.Install(path, script, 0777)
	if assert.NoError(t, err) {
		out, err = testqemu.VM.SSH(path)
		if assert.NoError(t, err) {
			assert.Equal(t, "hello | world\n", out)
		}
	}

	err = testqemu.Finalize()
	require.NoError(t, err)
}
