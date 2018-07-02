/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"os"
	"os/exec"
)

// CmdMonitor can be used to detect when a command terminates
// unexpectedly. It works by letting the command inherit the write
// end of a pipe, then closing that end in the parent process and then
// watching the read end.
//
// Alternatively one can also block on cmd.Wait() in a goroutine.
// But that might have unintended side effects, like reaping the child.
// The advantage of CmdMonitor is that it doesn't interfere with
// the child lifecycle.
type CmdMonitor struct {
	pr *os.File
	pw *os.File
}

// AddCmdMonitor prepares the command for watching. Must be
// called before starting the command.
func AddCmdMonitor(cmd *exec.Cmd) (CmdMonitor, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return CmdMonitor{}, err
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, pw)
	return CmdMonitor{pr, pw}, nil
}

// Watch must be called after starting the command.
// The returned channel is closed once the command
// terminates.
func (cm CmdMonitor) Watch() <-chan interface{} {
	done := make(chan interface{})
	go func() {
		defer close(done)
		b := make([]byte, 1)
		cm.pr.Read(b)
	}()
	cm.pw.Close()
	return done
}
