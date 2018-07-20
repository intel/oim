/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package log

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/intel/oim/pkg/log/level"
)

func TestSimpleLogger(t *testing.T) {
	b := &bytes.Buffer{}
	l := NewSimpleLogger(SimpleConfig{level.Debug, b})
	l.Debug("hello", " ", "world")
	assert.Equal(t, "DEBUG hello world\n", b.String())

	b.Reset()
	l2 := l.With("foo", "bar", "x", "y").With("a", "b")
	l2.Debugf("%d", 1)
	assert.Equal(t, "DEBUG 1 | foo: bar x: y a: b\n", b.String())

	b.Reset()
	l.Debugw("hello world", "foo", "bar")
	assert.Equal(t, "DEBUG hello world | foo: bar\n", b.String())

	assert.Panics(t, func() {
		l.Panic("oh no")
	})
}
