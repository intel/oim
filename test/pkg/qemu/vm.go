/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package qemu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/intel/govmm/qemu"
	"github.com/pkg/errors"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/oim-common"
)

type VirtualMachine struct {
	QMP    *qemu.QMP
	Cmd    *exec.Cmd
	Stderr bytes.Buffer
	SSHCmd string
	done   <-chan interface{}
	image  string
	start  string
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

// QMPLog implements https://godoc.org/github.com/intel/govmm/qemu#QMPLog
type QMPLog struct{}

func (ql QMPLog) V(int32) bool {
	return true
}

func (ql QMPLog) Infof(format string, v ...interface{}) {
	log.L().Infof("GOVMM "+format, v...)
}

func (ql QMPLog) Warningf(format string, v ...interface{}) {
	log.L().Warnf("GOVMM "+format, v...)
}

func (ql QMPLog) Errorf(format string, v ...interface{}) {
	log.L().Errorf("GOVMM "+format, v...)
}

// UseQEMU sets up a VM instance so that SSH commands can be issued.
// The machine must be started separately.
func UseQEMU(image string) (*VirtualMachine, error) {
	var err error
	var vm VirtualMachine
	// Here we use the start script provided with the image.
	// In addition, we disable the serial console and instead
	// use stdin/out for QMP. That way we immediately detect
	// when something went wrong during startup. Kernel
	// messages get collected also via stderr and thus
	// end up in VM.Stderr.
	vm.image, err = filepath.Abs(image)
	if err != nil {
		return nil, err
	}
	vm.image = strings.TrimSuffix(vm.image, ".img")
	helperFile := func(prefix string) string {
		return filepath.Join(filepath.Dir(vm.image), prefix+filepath.Base(vm.image))
	}
	vm.start = helperFile("start-")
	vm.SSHCmd = helperFile("ssh-")
	return &vm, nil
}

// StartQEMU() returns a VM pointer if a virtual machine could be
// started, and error when starting failed, and nil for both when no
// image is configured and thus nothing can be started.
func StartQEMU(image string, qemuOptions ...string) (*VirtualMachine, error) {
	vm, err := UseQEMU(image)
	if err != nil {
		return nil, err
	}

	// We have to use a Unix domain socket for qemu.QMPStart.
	qmpSocket := vm.image + ".qmp"
	if err := os.Remove(qmpSocket); err != nil && !os.IsNotExist(err) {
		return nil, errors.Wrapf(err, "removing %s", qmpSocket)
	}

	// Here we use the start script provided with the image.
	// In addition, we disable the serial console and instead
	// use stdin/out for QMP. That way we immediately detect
	// when something went wrong during startup. Kernel
	// messages get collected also via stderr and thus
	// end up in VM.Stderr.
	args := []string{
		vm.start, vm.image + ".img",
		"-serial", "none",
		"-chardev", "stdio,id=mon0",
		"-serial", "file:" + filepath.Join(filepath.Dir(image), "serial.log"),
		"-qmp", "unix:" + qmpSocket + ",server,nowait",
	}
	args = append(args, qemuOptions...)
	log.L().Debugf("QEMU command: %q", args)
	vm.Cmd = exec.Command(args[0], args[1:]...)
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
	// If we succeeed, we stop the timer.
	timer := time.AfterFunc(60*time.Second, func() {
		vm.Cmd.Process.Kill()
	})

	cmdMonitor, err := oimcommon.AddCmdMonitor(vm.Cmd)
	if err != nil {
		return nil, errors.Wrap(err, "AddCmdMonitor")
	}
	if err = vm.Cmd.Start(); err != nil {
		return nil, cleanup(err)
	}
	vm.done = cmdMonitor.Watch()

	// Poll for the QMP socket to appear.
	for {
		_, err := os.Stat(qmpSocket)
		if err == nil {
			break
		} else if !os.IsNotExist(err) {
			return nil, cleanup(errors.Wrapf(err, "stat %s", qmpSocket))
		}
		select {
		case <-vm.done:
			return nil, cleanup(errors.New("QEMU terminated while waiting for QMP socket"))
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Connect to QMP socket.
	cfg := qemu.QMPConfig{
		Logger: QMPLog{},
	}
	q, _, err := qemu.QMPStart(context.Background(), qmpSocket, cfg, make(chan struct{}))
	if err != nil {
		return nil, cleanup(errors.Wrapf(err, "QMPStart"))
	}

	// This has to be the first command executed in a QMP session.
	err = q.ExecuteQMPCapabilities(context.Background())
	if err != nil {
		return nil, cleanup(errors.Wrapf(err, "ExecuteQMPCapabilities"))
	}
	vm.QMP = q

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
	return vm, nil
}

// Running returns true if the virtual machine instance is currently active.
func (vm *VirtualMachine) Running() bool {
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

func (vm *VirtualMachine) String() string {
	if vm == nil {
		return "*VirtualMachine{nil}"
	}
	result := vm.image
	if vm.Running() {
		result = result + " running"
	}
	if vm.Cmd != nil && vm.Cmd.Process != nil {
		result = fmt.Sprintf("%s %d", result, vm.Cmd.Process.Pid)
	}
	return result
}

// Executes a shell command inside the virtual machine via ssh, using the helper
// script of the machine image. It returns the commands combined output and
// any exit error. Beware that (as usual) ssh will cocatenate the arguments
// and run the result in a shell, so complex scripts may break.
func (vm *VirtualMachine) SSH(args ...string) (string, error) {
	log.L().Debugf("Running SSH %s %s\n", vm.SSHCmd, args)
	cmd := exec.Command(vm.SSHCmd, args...)
	out, err := cmd.CombinedOutput()
	log.L().Debugf("Exit error: %v\nOutput: %s\n", err, string(out))
	return string(out), err
}

// Transfers the content to the virtual machine and creates the file
// with the chosen mode.
func (vm *VirtualMachine) Install(path string, data io.Reader, mode os.FileMode) error {
	cmd := exec.Command(vm.SSHCmd, fmt.Sprintf("rm -f '%[1]s' && cat > '%[1]s' && chmod %d '%s'", path, mode, path))
	cmd.Stdin = data
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("Installing %s failed: %s", path, out))
	}
	return nil
}

// StopQEMU ensures that the virtual machine powers down cleanly and
// all resources are freed. Can be called more than once.
func (vm *VirtualMachine) StopQEMU() error {
	var err error

	// Trigger shutdown, ignoring errors.
	// Give VM some time to power down, then kill it.
	if vm.Cmd != nil && vm.Cmd.Process != nil {
		timer := time.AfterFunc(10*time.Second, func() {
			log.L().Debugf("Cancelling")
			vm.Cmd.Process.Kill()
		})
		defer timer.Stop()
		log.L().Debugf("Powering down QEMU")
		vm.Cmd.Process.Signal(os.Interrupt)
		log.L().Debugf("Waiting for completion")
		err = vm.Cmd.Wait()
		vm.Cmd = nil
	}

	return err
}

type forwardPort struct {
	ssh        *exec.Cmd
	logWriter  io.Closer
	terminated <-chan interface{}
}

// ForwardPort activates port forwarding from a listen socket on the
// current host to another port inside the virtual machine. Errors can
// occur while setting up forwarding as well as later, in which case the
// returned channel will be closed. To stop port forwarding, call the
// io.Closer.
//
// The to and from specification can be ints (for ports) or strings (for
// Unix domaain sockets).
//
// Optionally a command can be run. If none is given, ssh is invoked with -N.
func (vm *VirtualMachine) ForwardPort(logger log.Logger, from interface{}, to interface{}, cmd ...string) (io.Closer, <-chan interface{}, error) {
	fromStr := portToString(from)
	toStr := portToString(to)
	args := []string{
		"-L", fmt.Sprintf("%s:%s", fromStr, toStr),
	}
	what := fmt.Sprintf("%.8s->%.8s", fromStr, toStr)
	if len(cmd) == 0 {
		args = append(args, "-N")
		what = what + "ssh"
	} else {
		args = append(args, cmd...)
		what = filepath.Base(cmd[0]) + " " + what
	}
	fp := forwardPort{
		// ssh closes all extra file descriptors, thus defeating our
		// CmdMonitor. Instead we wait for completion in a goroutine.
		ssh: exec.Command(vm.SSHCmd, args...),
	}
	out := oimcommon.LogWriter(logger.With("at", what))
	fp.ssh.Stdout = out
	fp.ssh.Stderr = out
	fp.logWriter = out
	terminated := make(chan interface{})
	fp.terminated = terminated
	if err := fp.ssh.Start(); err != nil {
		return nil, nil, err
	}
	go func() {
		defer close(terminated)
		fp.ssh.Wait()
	}()
	return &fp, terminated, nil
}

func portToString(port interface{}) string {
	if v, ok := port.(int); ok {
		return fmt.Sprintf("localhost:%d", v)
	}
	return fmt.Sprintf("%s", port)
}

func (fp *forwardPort) Close() error {
	fp.ssh.Process.Kill()
	<-fp.terminated
	fp.logWriter.Close()
	return nil
}
