/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

// qemu starts images under QEMU and controls the virtual machine
// via QMP. The main purpose is for testing, so a single instance
// is created on demand and reused for different tests.
package qemu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/digitalocean/go-qemu/qemu"
)

type VM struct {
	Domain  *qemu.Domain
	Monitor *StdioMonitor
	Cmd     *exec.Cmd
	Stderr  bytes.Buffer
	SSHCmd  string
	done    <-chan interface{}
}

type StartError struct {
	Args         []string
	Stderr       string
	ProcessState *os.ProcessState
	ExitError    error
	OtherError   error
}

func (err StartError) Error() string {
	return fmt.Sprintf("Problem with QEMU %s: %s\nCommand terminated: %s\n%s",
		err.Args,
		err.OtherError,
		err.ExitError,
		err.Stderr)
}

// StartQEMU() returns a VM pointer if a virtual machine could be
// started, and error when starting failed, and nil for both when no
// image is configured and thus nothing can be started.
func StartQEMU(image string, qemuOptions ...string) (*VM, error) {
	var err error
	var vm VM

	// Here we use the start script provided with the image.
	// In addition, we disable the serial console and instead
	// use stdin/out for QMP. That way we immediately detect
	// when something went wrong during startup. Kernel
	// messages get collected also via stderr and thus
	// end up in VM.Stderr.
	image, err = filepath.Abs(image)
	if err != nil {
		return nil, err
	}
	helperFile := func(prefix string) string {
		return filepath.Join(filepath.Dir(image), prefix+filepath.Base(image))
	}
	start := helperFile("start-")
	vm.SSHCmd = helperFile("ssh-")
	args := []string{
		start, image + ".img",
		"-serial", "none",
		"-chardev", "stdio,id=mon0",
		"-serial", "file:" + filepath.Join(filepath.Dir(image), "serial.log"),
		"-mon", "chardev=mon0,mode=control,pretty=off",
	}
	args = append(args, qemuOptions...)
	log.Printf("QEMU command: %q", args)
	vm.Cmd = exec.Command(args[0], args[1:]...)
	in, err := vm.Cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := vm.Cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	vm.Cmd.Stderr = &vm.Stderr

	// cleanup() kills the command and collects as much information as possible
	// in the resulting error.
	cleanup := func(err error) error {
		var exitErr error
		if vm.Cmd != nil {
			if vm.Cmd.Process != nil {
				vm.Cmd.Process.Kill()
			}
			exitErr = vm.Cmd.Wait()
		}
		return StartError{
			Args:         args,
			Stderr:       string(vm.Stderr.Bytes()),
			OtherError:   err,
			ExitError:    exitErr,
			ProcessState: vm.Cmd.ProcessState,
		}
	}

	// Give VM some time to power up, then kill it.
	timer := time.AfterFunc(60*time.Second, func() {
		vm.Cmd.Process.Kill()
	})

	if err = vm.Cmd.Start(); err != nil {
		return nil, cleanup(err)
	}
	if vm.Monitor, err = NewStdioMonitor(in, out); err != nil {
		return nil, cleanup(err)
	}
	if vm.done, err = vm.Monitor.ConnectStdio(); err != nil {
		return nil, cleanup(err)
	}
	if vm.Domain, err = qemu.NewDomain(vm.Monitor, filepath.Base(image)); err != nil {
		return nil, cleanup(err)
	}

	// Wait for successful SSH connection.
	for {
		if !vm.Running() {
			return nil, cleanup(errors.New("timed out waiting for SSH"))
		}
		_, err := vm.SSH("true")
		if err == nil {
			break
		}
	}

	timer.Stop()
	return &vm, nil
}

// Running returns true if the virtual machine instance is currently active.
func (vm *VM) Running() bool {
	if vm.done == nil {
		// Not started yet or already exited.
		return false
	}
	select {
	case <-vm.done:
		return false
	default:
		return true
	}
}

// Executes a shell command inside the virtual machine via ssh, using the helper
// script of the machine image. It returns the commands combined output and
// any exit error. Beware that (as usual) ssh will cocatenate the arguments
// and run the result in a shell, so complex scripts may break.
func (vm *VM) SSH(args ...string) (string, error) {
	log.Printf("Running SSH %s %s\n", vm.SSHCmd, args)
	cmd := exec.Command(vm.SSHCmd, args...)
	out, err := cmd.CombinedOutput()
	log.Printf("Exit error: %v\nOutput: %s\n", err, string(out))
	return string(out), err
}

// Transfers the content to the virtual machine and creates the file
// with the chosen mode.
func (vm *VM) Install(path string, data io.Reader, mode os.FileMode) error {
	cmd := exec.Command(vm.SSHCmd, fmt.Sprintf("cat > '%s' && chmod %d '%s'", path, mode, path))
	cmd.Stdin = data
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("Installing %s failed: %s", path, out))
	}
	return nil
}

// StopQEMU ensures that the virtual machine powers down cleanly and
// all resources are freed.
func (vm *VM) StopQEMU() error {
	// Trigger shutdown, ignoring errors.
	// Give VM some time to power down, then kill it.
	timer := time.AfterFunc(10*time.Second, func() {
		log.Printf("Cancelling")
		vm.Cmd.Process.Kill()
	})
	defer timer.Stop()
	log.Printf("Powering down QEMU")
	vm.Cmd.Process.Signal(os.Interrupt)
	log.Printf("Waiting for completion")
	err := vm.Cmd.Wait()

	vm = nil
	return err
}
