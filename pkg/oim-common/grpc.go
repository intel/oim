/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net"
	"strings"
	"time"

	// "github.com/grpc-ecosystem/go-grpc-middleware"
	// "github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	// "github.com/opentracing/opentracing-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/pkg/errors"
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

// LoadTLSConfig sets up the necessary TLS configuration for a
// client or server. The peer name must be set when expecting the
// peer to offer a certificate with that common name, otherwise it can
// be left empty.
//
// caFile must be the full file name. keyFile can either be the .crt
// file (foo.crt, implies foo.key) or the base name (foo for foo.crt
// and foo.key).
func LoadTLSConfig(caFile, key, peerName string) (*tls.Config, error) {
	var base string
	if strings.HasSuffix(key, ".key") || strings.HasSuffix(key, ".crt") {
		base = key[0 : len(key)-4]
	} else {
		base = key
	}
	crtFile := base + ".crt"
	keyFile := base + ".key"
	certificate, err := tls.LoadX509KeyPair(crtFile, keyFile)
	if err != nil {
		return nil, errors.Wrapf(err, "load X509 key pair for key=%q", key)
	}

	certPool := x509.NewCertPool()
	bs, err := ioutil.ReadFile(caFile)
	if err != nil {
		return nil, errors.Wrap(err, "read CA cert")
	}

	ok := certPool.AppendCertsFromPEM(bs)
	if !ok {
		return nil, errors.Errorf("failed to append certs from %q", caFile)
	}

	tlsConfig := &tls.Config{
		ServerName:   peerName,
		Certificates: []tls.Certificate{certificate},
		RootCAs:      certPool,
		ClientCAs:    certPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	return tlsConfig, nil
}

// LoadTLS is identical to LoadTLSConfig except that it returns
// the TransportCredentials for a gRPC client or server.
func LoadTLS(caFile, key, peerName string) (credentials.TransportCredentials, error) {
	tlsConfig, err := LoadTLSConfig(caFile, key, peerName)
	if err != nil {
		return nil, err
	}
	return credentials.NewTLS(tlsConfig), nil
}
