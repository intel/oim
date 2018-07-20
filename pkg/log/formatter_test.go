/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package log

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/intel/oim/pkg/log/level"
)

func TestFormatter(t *testing.T) {
	f := Formatter{}

	assert.Equal(t, "DEBUG hello world\n",
		string(f.Print(level.Debug, Fields{}, "hello world")))
	assert.Equal(t, "INFO hello world\n",
		string(f.Printf(level.Info, Fields{}, "%s", "hello world")))
	assert.Equal(t, "WARN hello world\n",
		string(f.Printw(level.Warn, Fields{}, "hello world")))
	assert.Equal(t, "FATAL hello world\n",
		string(f.Printw(level.Warn, Fields{}, "hello world", "level", level.Fatal)))
	assert.Equal(t, "12:00 WARN hello world\n",
		string(f.Printw(level.Warn, Fields{Field{"time", "12:00"}}, "hello world")))
	assert.Equal(t, "12:00 WARN foo: hello world\n",
		string(f.Printw(level.Warn, Fields{Field{"time", "12:00"}}, "hello world", "at", "foo")))
	assert.Equal(t, "12:00 WARN foo/bar: hello world\n",
		string(f.Printw(level.Warn, Fields{Field{"time", "12:00"}}, "hello world", "at", "foo", "at", "bar")))
	assert.Equal(t, "12:00 WARN foo/bar: hello world | x: y\n",
		string(f.Printw(level.Warn, Fields{Field{"time", "12:00"}}, "hello world", "at", "foo", "at", "bar", "x", "y")))
}
