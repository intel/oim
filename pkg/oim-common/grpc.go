/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"context"
	"net"
	"strings"
	"time"
)

// GRPCDialer can be used with grpc.WithDialer. It supports
// addresses of the format defined for ParseEndpoint.
// Necessary because of https://github.com/grpc/grpc-go/issues/1741.
func GRPCDialer(endpoint string, t time.Duration) (net.Conn, error) {
	network, address, err := ParseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(t))
	defer cancel()
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

// ChooseDialer returns GRPCDialer if needed for the endpoint,
// otherwise nil for the default behavior.
func ChooseDialer(endpoint string) func(string, time.Duration) (net.Conn, error) {
	if strings.HasPrefix(endpoint, "unix://") {
		return GRPCDialer
	}
	return nil
}
