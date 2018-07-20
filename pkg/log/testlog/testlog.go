/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

// Package testlog provides a logger that outputs
// via testing.T.Log. There are two ways to use it
// inside a test function:
// - `defer testlog.SetGlobal(t)()` will install
//   a test logger as global logger and restore
//   the previous one when done. This only works
//   when not running tests inside the package
//   in parallel.
// - `ctx := log.WithLogger(testlog.New(t))`
//   creates a context which uses the test logger.
//   This works for parallel testing if all
//   logging happening inside the test is using
//   that context and the logger added to it.
package testlog

import (
	"io"
	"testing"

	"github.com/intel/oim/pkg/log"
	"github.com/intel/oim/pkg/log/level"
)

type testLogger struct {
	fields    log.Fields
	formatter log.Formatter
	t         *testing.T
}

// New creates a new test logger.
func New(t *testing.T) log.Logger {
	return new(t)
}

// SetGlobal creates a new test logger, installs
// it as the global logger, and returns a function
// which restores the previous global logger.
// The logger prints everything.
// It can be called with either a testing.T pointer
// or a io.Writer.
func SetGlobal(out interface{}) func() {
	old := log.L()
	var new log.Logger
	switch out.(type) {
	case *testing.T:
		new = New(out.(*testing.T))
	case io.Writer:
		new = log.NewSimpleLogger(log.SimpleConfig{
			Level:  level.Min,
			Output: out.(io.Writer),
		})
	default:
		panic("unsupported output type")
	}
	log.Set(new)
	return func() {
		log.Set(old)
	}
}

func new(t *testing.T) *testLogger {
	l := &testLogger{t: t}
	return l
}

func stripLF(buffer []byte) string {
	return string(buffer[0 : len(buffer)-1])
}

func (tl *testLogger) Output(threshold log.Threshold, args ...interface{}) {
	tl.t.Helper()
	tl.t.Log(stripLF(tl.formatter.Print(threshold, tl.fields, args...)))
	if threshold >= level.Fatal {
		tl.t.FailNow()
	}
}

func (tl *testLogger) Outputf(threshold log.Threshold, format string, args ...interface{}) {
	tl.t.Helper()
	tl.t.Log(stripLF(tl.formatter.Printf(threshold, tl.fields, format, args...)))
	if threshold >= level.Fatal {
		tl.t.FailNow()
	}
}

func (tl *testLogger) Outputw(threshold log.Threshold, msg string, keysAndValues ...interface{}) {
	tl.t.Helper()
	tl.t.Log(stripLF(tl.formatter.Printw(threshold, tl.fields, msg, keysAndValues...)))
	if threshold >= level.Fatal {
		tl.t.FailNow()
	}
}

// With creates a new instance with the same testing.T and the
// additional fields added.
func (tl *testLogger) With(keysAndValues ...interface{}) log.Logger {
	tl2 := new(tl.t)
	tl2.fields = tl.fields.Clone(keysAndValues...)
	return tl2
}

// We have to provide our own functions instead of using
// LoggerBase because we have to mark each function as
// a test helper. Otherwise the log entry would list
// the source code line of the helper function instead
// of the caller.

func (tl *testLogger) Debug(args ...interface{}) {
	tl.t.Helper()
	tl.Output(level.Debug, args...)
}

func (tl *testLogger) Debugf(format string, args ...interface{}) {
	tl.t.Helper()
	tl.Outputf(level.Debug, format, args...)
}

func (tl *testLogger) Debugw(msg string, keysAndValues ...interface{}) {
	tl.t.Helper()
	tl.Outputw(level.Debug, msg, keysAndValues...)
}

func (tl *testLogger) Info(args ...interface{}) {
	tl.t.Helper()
	tl.Output(level.Info, args...)
}

func (tl *testLogger) Infof(format string, args ...interface{}) {
	tl.t.Helper()
	tl.Outputf(level.Info, format, args...)
}

func (tl *testLogger) Infow(msg string, keysAndValues ...interface{}) {
	tl.t.Helper()
	tl.Outputw(level.Info, msg, keysAndValues...)
}

func (tl *testLogger) Warn(args ...interface{}) {
	tl.t.Helper()
	tl.Output(level.Warn, args...)
}

func (tl *testLogger) Warnf(format string, args ...interface{}) {
	tl.t.Helper()
	tl.Outputf(level.Warn, format, args...)
}

func (tl *testLogger) Warnw(msg string, keysAndValues ...interface{}) {
	tl.t.Helper()
	tl.Outputw(level.Warn, msg, keysAndValues...)
}

func (tl *testLogger) Error(args ...interface{}) {
	tl.t.Helper()
	tl.Output(level.Error, args...)
}

func (tl *testLogger) Errorf(format string, args ...interface{}) {
	tl.t.Helper()
	tl.Outputf(level.Error, format, args...)
}

func (tl *testLogger) Errorw(msg string, keysAndValues ...interface{}) {
	tl.t.Helper()
	tl.Outputw(level.Error, msg, keysAndValues...)
}

func (tl *testLogger) Fatal(args ...interface{}) {
	tl.t.Helper()
	tl.Output(level.Fatal, args...)
}

func (tl *testLogger) Fatalf(format string, args ...interface{}) {
	tl.t.Helper()
	tl.Outputf(level.Fatal, format, args...)
}

func (tl *testLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	tl.t.Helper()
	tl.Outputw(level.Fatal, msg, keysAndValues...)
}

func (tl *testLogger) Panic(args ...interface{}) {
	tl.t.Helper()
	tl.Output(level.Panic, args...)
}

func (tl *testLogger) Panicf(format string, args ...interface{}) {
	tl.t.Helper()
	tl.Outputf(level.Panic, format, args...)
}

func (tl *testLogger) Panicw(msg string, keysAndValues ...interface{}) {
	tl.t.Helper()
	tl.Outputw(level.Panic, msg, keysAndValues...)
}
