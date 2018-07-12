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

	// "github.com/grpc-ecosystem/go-grpc-middleware"
	// "github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	// "github.com/opentracing/opentracing-go"
	"google.golang.org/grpc"
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

// ChooseDialOpts sets certain default options for the given endpoint,
// then adds the ones given as additional parameters. For unix://
// endpoints it activates the custom dialer and disables security.
func ChooseDialOpts(endpoint string, opts ...grpc.DialOption) []grpc.DialOption {
	result := []grpc.DialOption{}

	if strings.HasPrefix(endpoint, "unix://") {
		result = append(result,
			grpc.WithDialer(GRPCDialer),
			grpc.WithInsecure(),
		)
	} else {
		// TODO: enable security for tcp
		result = append(result,
			grpc.WithInsecure(),
		)
	}

	// TODO: kubernetes-csi uses grpc.WithBackoffMaxDelay(time.Second),
	// should we do the same?

	// Tracing of outgoing calls, including remote and local logging.
	formatter := CompletePayloadFormatter{} // TODO: filter out secrets
	// interceptor := grpc_middleware.ChainUnaryClient(
	// 	otgrpc.OpenTracingClientInterceptor(
	// 		opentracing.GlobalTracer(),
	// 		otgrpc.SpanDecorator(TraceGRPCPayload(formatter))),
	// 	LogGRPCClient(formatter))
	interceptor := LogGRPCClient(formatter)
	opts = append(opts, grpc.WithUnaryInterceptor(interceptor))

	result = append(result, opts...)
	return result
}
