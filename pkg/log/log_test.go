/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package log

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLog(t *testing.T) {
	ctx := context.Background()

	assert.Equal(t, L(), FromContext(ctx), "context without logger must return global logger")
	ctx2 := With(ctx)
	assert.NotEqual(t, L(), FromContext(ctx2), "context with logger must return that logger")
}
