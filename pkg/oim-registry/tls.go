/*
Copyright (C) 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimregistry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// RegistryClientContext creates a new context with credentials as if
// the client had connected via TLS with the client name as
// CommonName.
func RegistryClientContext(ctx context.Context, client string) context.Context {
	return peer.NewContext(ctx, &peer.Peer{
		AuthInfo: credentials.TLSInfo{
			State: tls.ConnectionState{
				VerifiedChains: [][]*x509.Certificate{{&x509.Certificate{Subject: pkix.Name{CommonName: client}}}},
			},
		},
	})
}
