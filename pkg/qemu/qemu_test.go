/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package qemu_test

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/intel/oim/pkg/qemu"
	testqemu "github.com/intel/oim/test/pkg/qemu"
)

type MemLogger struct {
	bytes.Buffer
}

func (m *MemLogger) Log(args ...interface{}) {
	fmt.Fprintln(&m.Buffer, args...)
}
func (m *MemLogger) Logf(format string, args ...interface{}) {
	fmt.Fprintln(&m.Buffer, fmt.Sprintf(format, args...))
}

func TestQEMU(t *testing.T) {
	var err error

	err = testqemu.Init(testqemu.WithLogger(t))
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

	{
		var sshLog MemLogger
		fp, terminated, err := testqemu.VM.ForwardPort(&sshLog, -1, 80)
		require.NoError(t, err)
		select {
		case <-terminated:
			// This is expected.
		case <-time.After(5 * time.Second):
			t.Fatal("ssh forwarding of port -1 did not fail")
		}
		fp.Close()
		out := sshLog.String()
		assert.NotEqual(t, "", out)
	}

	{
		// Port 11666 is what we expect to be available for
		// OIM CSI driver testing, see
		// test/e2e/storage/oim-csi.go.
		fp, terminated, err := testqemu.VM.ForwardPort(t, 11666, 8765)
		require.NoError(t, err)
		var socatOut string
		var socatErr error
		done := make(chan interface{})
		go func() {
			defer close(done)
			socatOut, socatErr = testqemu.VM.SSH("socat", "TCP-LISTEN:8765", "STDOUT")
		}()
		// Wait for socat to be ready.
	loop:
		for {
			select {
			case <-done:
				t.Fatal("socat terminated unexpectedly")

			case <-time.After(10 * time.Millisecond):
				out, _ := testqemu.VM.SSH("netstat", "-l", "-t", "-n")
				if strings.Contains(out, ":8765") {
					break loop
				}
			}
		}

		// Now send something to socat via our port forwarding.
		conn, err := net.Dial("tcp", ":11666")
		require.NoError(t, err)
		hello := "hello world"
		written, err := conn.Write([]byte(hello))
		require.NoError(t, err)
		require.Equal(t, len(hello), written)
		err = conn.Close()
		require.NoError(t, err)

		select {
		case <-done:
			t.Log("socat terminated")

		case <-terminated:
			t.Fatal("port forwarding failed")
		}
		assert.NoError(t, socatErr)
		assert.Equal(t, hello, socatOut)

		err = fp.Close()
		assert.NoError(t, err)
	}

	err = testqemu.Finalize()
	require.NoError(t, err)
}

func TestQEMUFailure(t *testing.T) {
	start := time.Now()
	_, err := qemu.StartQEMU("no-such-image")
	end := time.Now()
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "no-such-image")
	}
	assert.WithinDuration(t, end, start, time.Second)
}
