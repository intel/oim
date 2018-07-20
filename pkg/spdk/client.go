/*
Copyright (C) 2018 Intel Corporation
SPDX-License-Identifier: Apache-2.0

This file contains code from the Go distribution, under:
SPDX-License-Identifier: BSD-3-Clause

More specifically, this file is a copy of net/rpc/json/client.go,
updated to encode messages such that SPDK accepts them (jsonrpc,
params, etc.).

The original license text is as follows:
     Copyright 2010 The Go Authors.

     Redistribution and use in source and binary forms, with or without
     modification, are permitted provided that the following conditions are
     met:

        * Redistributions of source code must retain the above copyright
     notice, this list of conditions and the following disclaimer.
        * Redistributions in binary form must reproduce the above
     copyright notice, this list of conditions and the following disclaimer
     in the documentation and/or other materials provided with the
     distribution.
        * Neither the name of Google Inc. nor the names of its
     contributors may be used to endorse or promote products derived from
     this software without specific prior written permission.

     THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
     "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
     LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
     A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
     OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
     SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
     LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
     DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
     THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
     (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
     OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/

package spdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/rpc"
	"regexp"
	"strconv"
	"sync"

	"github.com/intel/oim/pkg/log"
)

// From SPDK's include/spdk/jsonrpc.h:
const (
	ERROR_PARSE_ERROR      = -32700
	ERROR_INVALID_REQUEST  = -32600
	ERROR_METHOD_NOT_FOUND = -32601
	ERROR_INVALID_PARAMS   = -32602
	ERROR_INTERNAL_ERROR   = -32603

	ERROR_INVALID_STATE = -1
)

// jsonError matches against errors strings as encoded by ReadResponseHeader.
var jsonError = regexp.MustCompile(`^code: (-?\d+) msg: (.*)$`)

// IsJSONError checks that the error has the expected error code. Use
// code == 0 to check for any JSONError.
func IsJSONError(err error, code int) bool {
	m := jsonError.FindStringSubmatch(err.Error())
	if m == nil {
		return false
	}
	errorCode, ok := strconv.Atoi(m[1])
	if ok != nil {
		return false
	}
	return code == 0 || errorCode == code
}

type clientCodec struct {
	dec *json.Decoder // for reading JSON values
	enc *json.Encoder // for writing JSON values
	c   io.Closer

	// temporary work space
	req  clientRequest
	resp clientResponse

	// JSON-RPC responses include the request id but not the request method.
	// Package rpc expects both.
	// We save the request method in pending when sending a request
	// and then look it up by request ID when filling out the rpc Response.
	mutex   sync.Mutex        // protects pending
	pending map[uint64]string // map request id to method name
}

// newClientCodec returns a new rpc.ClientCodec using JSON-RPC on conn.
func newClientCodec(conn io.ReadWriteCloser) rpc.ClientCodec {
	return &clientCodec{
		dec:     json.NewDecoder(conn),
		enc:     json.NewEncoder(conn),
		c:       conn,
		req:     clientRequest{Version: "2.0"},
		pending: make(map[uint64]string),
	}
}

// clientRequest represents the payload sent to the server. Compared to
// net/rpc/json, two changes were made:
// - add Version (aka jsonrpc)
// - change Params from list to a single value
// - Params must be a pointer so that we can use nil to
//   suppress the creation of the "params" entry (as expected by e.g. get_nbd_disks)
type clientRequest struct {
	Version string       `json:"jsonrpc"`
	Method  string       `json:"method"`
	Params  *interface{} `json:"params,omitempty"`
	Id      uint64       `json:"id"`
}

func (c *clientCodec) WriteRequest(r *rpc.Request, param interface{}) error {
	c.mutex.Lock()
	c.pending[r.Seq] = r.ServiceMethod
	c.mutex.Unlock()
	c.req.Method = r.ServiceMethod
	if param == nil {
		c.req.Params = nil
	} else {
		c.req.Params = &param
	}
	c.req.Id = r.Seq
	return c.enc.Encode(&c.req)
}

type clientResponse struct {
	Id     uint64           `json:"id"`
	Result *json.RawMessage `json:"result"`
	Error  interface{}      `json:"error"`
}

func (r *clientResponse) reset() {
	r.Id = 0
	r.Result = nil
	r.Error = nil
}

// ReadResponseHeader parses the response from SPDK. Returning
// an error here is treated as a failed connection, so we can only
// do that for real connection problems.
func (c *clientCodec) ReadResponseHeader(r *rpc.Response) error {
	c.resp.reset()
	if err := c.dec.Decode(&c.resp); err != nil {
		return err
	}

	c.mutex.Lock()
	r.ServiceMethod = c.pending[c.resp.Id]
	delete(c.pending, c.resp.Id)
	c.mutex.Unlock()

	r.Error = ""
	r.Seq = c.resp.Id
	if c.resp.Error != nil || c.resp.Result == nil {
		// SPDK returns a map[string]interface {}
		// with "code" and "message" as keys.
		m, ok := c.resp.Error.(map[string]interface{})
		if ok {
			code, haveCode := m["code"]
			message, haveMessage := m["message"]
			if !haveCode || !haveMessage {
				return fmt.Errorf("invalid error %v", c.resp.Error)
			}
			var codeVal int
			switch code.(type) {
			case int:
				codeVal = code.(int)
			case float64:
				codeVal = int(code.(float64))
			default:
				haveCode = false
			}
			messageVal, haveMessage := message.(string)
			if !haveCode || !haveMessage {
				return fmt.Errorf("invalid error content %v", c.resp.Error)
			}
			// It would be nice to return the real error code through
			// net/rpc, but it only supports simple strings. Therefore
			// we have to encode the available information as string.
			r.Error = fmt.Sprintf("code: %d msg: %s", codeVal, messageVal)
		} else {
			// The following code is from the original
			// net/rpc/json: it expects a simple string
			// as error.
			x, ok := c.resp.Error.(string)
			if !ok {
				return fmt.Errorf("invalid error %v", c.resp.Error)
			}
			if x == "" {
				x = "unspecified error"
			}
			r.Error = x
		}
	}
	return nil
}

func (c *clientCodec) ReadResponseBody(x interface{}) error {
	if x == nil {
		return nil
	}
	return json.Unmarshal(*c.resp.Result, x)
}

func (c *clientCodec) Close() error {
	return c.c.Close()
}

type Client struct {
	client *rpc.Client
}

type logConn struct {
	net.Conn
	logger log.Logger
}

func (lc *logConn) Read(b []byte) (int, error) {
	n, err := lc.Conn.Read(b)
	if err == nil {
		lc.logger.Debugw("read", "data", log.LineBuffer(b[:n]))
	} else if err != io.EOF {
		lc.logger.Errorw("read error", "error", err)
	}
	return n, err
}
func (lc *logConn) Write(b []byte) (int, error) {
	lc.logger.Debugw("write", "data", log.LineBuffer(b))
	n, err := lc.Conn.Write(b)
	if err != nil {
		lc.logger.Errorw("write error", "error", err)
	}
	return n, err
}

func New(path string) (*Client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	conn = &logConn{conn, log.L().With("at", "spdk-rpc")}
	client := rpc.NewClientWithCodec(newClientCodec(conn))
	return &Client{client: client}, nil
}

func (c *Client) Close() {
	c.client.Close()
}

func (c *Client) Invoke(_ context.Context, method string, args, reply interface{}) error {
	return c.client.Call(method, args, reply)
}
