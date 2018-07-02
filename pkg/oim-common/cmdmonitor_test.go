/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCmdMonitor(t *testing.T) {
	var err error

	cmd := exec.Command("true")
	cmdMonitor, err := AddCmdMonitor(cmd)
	require.NoError(t, err)

	err = cmd.Start()
	require.NoError(t, err)
	terminated := cmdMonitor.Watch()

	select {
	case <-terminated:
		// Okay.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for CmdMonitor")
	}
	cmd.Process.Kill()
	cmd.Wait()
}

func TestNoCmdMonitor(t *testing.T) {
	var err error

	cmd := exec.Command("sleep", "10000")
	cmdMonitor, err := AddCmdMonitor(cmd)
	require.NoError(t, err)

	err = cmd.Start()
	require.NoError(t, err)
	terminated := cmdMonitor.Watch()

	select {
	case <-terminated:
		// Not okay!
		t.Fatal("CmdMonitor triggered prematurely.")
	case <-time.After(5 * time.Second):
		// Okay.
	}
	cmd.Process.Kill()
	cmd.Wait()
}
