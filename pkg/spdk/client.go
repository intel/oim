/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package spdk

import (
	"context"
	"encoding/json"
	"io"
	"net"

	"github.com/mafredri/cdp/rpcc"
)

type Client struct {
	conn *rpcc.Conn
}

// json2Codec implements the Codec interface, with one small
// twist: it adds "jsonrpc": "2.0" to outgoing messages.
type json2Codec struct {
	enc *json.Encoder
	dec *json.Decoder
}

func (c *json2Codec) WriteRequest(r *rpcc.Request) error {
	//	type json2Request struct {
	//		jsonrpc string
	//		rpcc.Request
	//	}
	//	return c.enc.Encode(&json2Request{jsonrpc: "2.0", Request: *r})
	type json2Request struct {
		JSONRpc string      `json:"jsonrpc"`
		ID      uint64      `json:"id"`               // ID chosen by client.
		Method  string      `json:"method"`           // Method invoked on remote.
		Args    interface{} `json:"params,omitempty"` // Method parameters, if any.
	}
	r2 := json2Request{
		JSONRpc: "2.0",
		ID:      r.ID,
		Method:  r.Method,
		Args:    r.Args,
	}
	return c.enc.Encode(&r2)
}
func (c *json2Codec) ReadResponse(r *rpcc.Response) error { return c.dec.Decode(r) }
func newJSON2Codec(conn io.ReadWriter) rpcc.Codec {
	return &json2Codec{
		enc: json.NewEncoder(conn),
		dec: json.NewDecoder(conn),
	}
}

func New(path string) (*Client, error) {
	netDial := func(ctx context.Context, addr string) (io.ReadWriteCloser, error) {
		conn, err := net.Dial("unix", addr)
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
	conn, err := rpcc.Dial(path, rpcc.WithDialer(netDial), rpcc.WithCodec(newJSON2Codec))
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

func (c *Client) Close() {
	c.conn.Close()
}

func (c *Client) Invoke(ctx context.Context, method string, args, reply interface{}) error {
	return rpcc.Invoke(ctx, method, args, reply, c.conn)
}
