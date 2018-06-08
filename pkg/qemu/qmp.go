/*
Copyright 2016 The go-qemu Authors.
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0

This is based on https://github.com/digitalocean/go-qemu/blob/9b21eec6749f917c9a91df05cae2f2f301983b27/qmp/socket.go
and was modified so that it can use separate, existing IO streams.
*/

package qemu

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"sync"
	"sync/atomic"

	"github.com/digitalocean/go-qemu/qmp"
)

// A StdioMonitor is a Monitor which speaks directly to a QEMU Machine Protocol
// (QMP) that was set up for stdin/out. Multiple connections to the
// same domain are not permitted, and will result in the monitor blocking until
// the existing connection is closed.
type StdioMonitor struct {
	// QEMU version reported by a connected monitor socket.
	Version *qmp.Version

	// Underlying connection
	in  io.WriteCloser
	out io.ReadCloser

	// Serialize running command against domain
	mu sync.Mutex

	// Send command responses and errors
	stream <-chan streamResponse

	// Send domain events to listeners when available
	listeners *int32
	events    <-chan qmp.Event
}

// NewStdioMonitor configures a connection to the provided QEMU input/output
// streams.
func NewStdioMonitor(in io.WriteCloser, out io.ReadCloser) (*StdioMonitor, error) {
	mon := &StdioMonitor{
		in:        in,
		out:       out,
		listeners: new(int32),
	}

	return mon, nil
}

// Disconnect closes the QEMU input channel.
func (mon *StdioMonitor) Disconnect() error {
	atomic.StoreInt32(mon.listeners, 0)
	err := mon.in.Close()

	return err
}

// qmpCapabilities is the command which must be executed to perform the
// QEMU QMP handshake.
const qmpCapabilities = "qmp_capabilities"

// Connect sets up a QEMU QMP connection by connecting directly to the QEMU
// monitor socket.  An error is returned if the capabilities handshake does
// not succeed. The returned channel will be closed once the connection
// to the command gets lost.
func (mon *StdioMonitor) ConnectStdio() (<-chan interface{}, error) {
	enc := json.NewEncoder(mon.in)
	dec := json.NewDecoder(mon.out)

	// Check for banner on startup
	var ban banner
	if err := dec.Decode(&ban); err != nil {
		return nil, err
	}
	mon.Version = &ban.QMP.Version

	// Issue capabilities handshake
	cmd := qmp.Command{Execute: qmpCapabilities}
	if err := enc.Encode(cmd); err != nil {
		return nil, err
	}

	// Check for no error on return
	var r response
	if err := dec.Decode(&r); err != nil {
		return nil, err
	}
	if err := r.Err(); err != nil {
		return nil, err
	}

	// Initialize socket listener for command responses and asynchronous
	// events
	events := make(chan qmp.Event)
	stream := make(chan streamResponse)
	done := make(chan interface{})
	go mon.listen(mon.out, events, stream, done)

	mon.events = events
	mon.stream = stream

	return done, nil
}
func (mon *StdioMonitor) Connect() error {
	_, err := mon.ConnectStdio()
	return err
}

// Events streams QEMU QMP Events.
// Events should only be called once per Socket.  If used with a qemu.Domain,
// qemu.Domain.Events should be called to retrieve events instead.
func (mon *StdioMonitor) Events() (<-chan qmp.Event, error) {
	atomic.AddInt32(mon.listeners, 1)
	return mon.events, nil
}

// listen listens for incoming data from a QEMU monitor socket.  It determines
// if the data is an asynchronous event or a response to a command, and returns
// the data on the appropriate channel.
func (mon *StdioMonitor) listen(r io.Reader, events chan<- qmp.Event, stream chan<- streamResponse, done chan<- interface{}) {
	defer close(events)
	defer close(stream)
	defer close(done)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		var e qmp.Event

		b := scanner.Bytes()
		log.Printf("QEMU <: %s", string(b))
		if err := json.Unmarshal(b, &e); err != nil {
			continue
		}

		// If data does not have an event type, it must be in response to a command.
		if e.Event == "" {
			stream <- streamResponse{buf: b}
			continue
		}

		// If nobody is listening for events, do not bother sending them.
		if atomic.LoadInt32(mon.listeners) == 0 {
			continue
		}

		events <- e
	}

	if err := scanner.Err(); err != nil {
		stream <- streamResponse{err: err}
	}
}

// Run executes the given QAPI command against a domain's QEMU instance.
// For a list of available QAPI commands, see:
//	http://git.qemu.org/?p=qemu.git;a=blob;f=qapi-schema.json;hb=HEAD
func (mon *StdioMonitor) Run(command []byte) ([]byte, error) {
	// Only allow a single command to be run at a time to ensure that responses
	// to a command cannot be mixed with responses from another command
	mon.mu.Lock()
	defer mon.mu.Unlock()

	log.Printf("QEMU >: %s", string(command))
	if _, err := mon.in.Write(command); err != nil {
		return nil, err
	}

	// Wait for a response or error to our command
	res := <-mon.stream
	if res.err != nil {
		return nil, res.err
	}

	// Check for QEMU errors
	var r response
	if err := json.Unmarshal(res.buf, &r); err != nil {
		return nil, err
	}
	if err := r.Err(); err != nil {
		return nil, err
	}

	return res.buf, nil
}

// banner is a wrapper type around a Version.
type banner struct {
	QMP struct {
		Version qmp.Version `json:"version"`
	} `json:"QMP"`
}

// streamResponse is a struct sent over a channel in response to a command.
type streamResponse struct {
	buf []byte
	err error
}

type response struct {
	ID     string      `json:"id"`
	Return interface{} `json:"return,omitempty"`
	Error  struct {
		Class string `json:"class"`
		Desc  string `json:"desc"`
	} `json:"error,omitempty"`
}

func (r *response) Err() error {
	if r.Error.Desc == "" {
		return nil
	}

	return errors.New(r.Error.Desc)
}
