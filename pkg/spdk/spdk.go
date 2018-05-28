/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

// spdk provides Go bindings for the SPDK JSON 2.0 RPC interface
// (http://www.spdk.io/doc/jsonrpc.html).
package spdk

import (
	"context"
)

type GetBDevsArgs struct {
	Name string `json:"name,omitempty"`
}

type SupportedIOTypes struct {
	Read       bool `json:"read"`
	Write      bool `json:"write"`
	Unmap      bool `json:"unmap"`
	WriteZeros bool `json:"write_zeroes"`
	Flush      bool `json:"flush"`
	Reset      bool `json:"reset"`
	NVMEAdmin  bool `json:"nvme_admin"`
	NVMEIO     bool `json:"nvme_io"`
}

type BDev struct {
	Name             string           `json:"name"`
	ProductName      string           `json:"product_name"`
	UUID             string           `json:"uuid"`
	BlockSize        int64            `json:"block_size"`
	NumBlocks        int64            `json:"num_blocks"`
	Claimed          bool             `json:"claimed"`
	SupportedIOTypes SupportedIOTypes `json:"supported_io_types"`
}

type GetBDevsResponse []BDev

func GetBDevs(ctx context.Context, client *Client, args *GetBDevsArgs) (GetBDevsResponse, error) {
	var response GetBDevsResponse
	// nil gets encoded as "params": null, which spdk doesn't
	// accept. The empty struct however is fine (in
	// v18.04-126-g9a9bef0a, thanks to
	// https://github.com/spdk/spdk/issues/303)
	if args == nil {
		args = &GetBDevsArgs{}
	}
	err := client.Invoke(ctx, "get_bdevs", args, &response)
	if err != nil {
		return nil, err
	}
	return response, nil
}
