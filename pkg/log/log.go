/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

// Package log provides an interface for a structured logger like the
// SugaredLogger in go.uber.org/zap or sirupsen/logrus. The application
// developer decides how to provide such a logger. Libraries use this
// package to get a logger from their current context (preferred) or
// use the global default.
//
// This goes beyond the idea proposed in
// https://blog.gopheracademy.com/advent-2016/context-logging/ where a
// structured logger implementation is set globally and then used to
// log values attached to a context. In this package, the logger
// *itself* is attached to the context.  This allows different
// contexts to use different loggers, for example with different
// settings.
package log

import (
	"context"
	"io"
	"log"
	"os"
	"sync"

	"github.com/intel/oim/pkg/log/level"
)

// Threshold defines the severity of a log or trace event.
// It is a type alias to simplify the usage of the log package.
type Threshold = level.Threshold

// Logger supports structured logging at different severity levels.
type Logger interface {
	// Debug uses fmt.Sprint to construct and log a message.
	Debug(args ...interface{})
	// Debugf uses fmt.Sprintf to log a templated message.
	Debugf(format string, args ...interface{})
	// Debugw logs a message with some additional context. The
	// variadic key-value pairs are treated as they are in With.
	Debugw(msg string, keysAndValues ...interface{})

	// Info uses fmt.Sprint to construct and log a message.
	Info(args ...interface{})
	// Infof uses fmt.Sprintf to log a templated message.
	Infof(format string, args ...interface{})
	// Infow logs a message with some additional context. The
	// variadic key-value pairs are treated as they are in With.
	Infow(msg string, keysAndValues ...interface{})

	// Warn uses fmt.Sprint to construct and log a message.
	Warn(args ...interface{})
	// Warnf uses fmt.Sprintf to log a templated message.
	Warnf(format string, args ...interface{})
	// Warnw logs a message with some additional context. The
	// variadic key-value pairs are treated as they are in With.
	Warnw(msg string, keysAndValues ...interface{})

	// Error uses fmt.Sprint to construct and log a message.
	Error(args ...interface{})
	// Errorf uses fmt.Sprintf to log a templated message.
	Errorf(format string, args ...interface{})
	// Errorw logs a message with some additional context. The
	// variadic key-value pairs are treated as they are in With.
	Errorw(msg string, keysAndValues ...interface{})

	// Fatal uses fmt.Sprint to construct and log a message.
	// It then quits by calling os.Exit(1).
	Fatal(args ...interface{})
	// Fatalf uses fmt.Sprintf to log a templated message.
	// It then quits by calling os.Exit(1).
	Fatalf(format string, args ...interface{})
	// Fatalw logs a message with some additional context. The
	// variadic key-value pairs are treated as they are in With.
	// It then quits by calling os.Exit(1).
	Fatalw(msg string, keysAndValues ...interface{})

	// Panic uses fmt.Sprint to construct and log a message.
	// It then quits by panicking.
	Panic(args ...interface{})
	// Panicf uses fmt.Sprintf to log a templated message.
	// It then quits by panicking.
	Panicf(format string, args ...interface{})
	// Panicw logs a message with some additional context. The
	// variadic key-value pairs are treated as they are in With.
	// It then quits by panicking.
	Panicw(msg string, keysAndValues ...interface{})

	// Output prints a message the same way as the function with
	// the same name as the threshold, using fmt.Sprint, then
	// exits or panics if needed.
	Output(threshold Threshold, args ...interface{})
	// Output prints a message the same way as the function with
	// the same name as the threshold, using fmt.Sprintf, then
	// exits or panics if needed.
	Outputf(threshold Threshold, format string, args ...interface{})
	// Output prints a message the same way as the function with
	// the same name as the threshold, using the same key-value
	// pairs as for With, then exits or panics if needed.
	Outputw(threshold Threshold, msg string, keysAndValues ...interface{})

	// With adds a variadic number of fields to the logging
	// context. It accepts loosely-typed key-value pairs. When
	// processing pairs, the first element of the pair is used as
	// the field key and the second as the field value.
	With(keysAndValues ...interface{}) Logger
}

var (
	logger = NewSimpleLogger(SimpleConfig{
		Level:  level.Warn,
		Output: os.Stdout,
	})
	mutex sync.Mutex
)

// L returns the current global logger.
//
// There are intentionally no helper functions (i.e. log.Info =
// log.L().Info), because those add another stack entry between the
// caller and the logger, which breaks loggers which record the source
// code location of their direct caller.
func L() Logger {
	mutex.Lock()
	defer mutex.Unlock()
	return logger
}

// Set sets the current global logger.
func Set(l Logger) {
	mutex.Lock()
	defer mutex.Unlock()
	logger = l
}

// SetOutput is a drop-in replacement for the traditional log.SetOutput:
// - it installs a simple logger which prints everything
// - it writes to the given writer
// - the output of traditional log calls are handled to the
//   simple logger, to get consistent logging
//
// This cannot be undone, because there is no function for getting
// the old io.Writer.
func SetOutput(writer io.Writer) {
	l := newSimpleLogger(SimpleConfig{
		Level:  level.Min,
		Output: writer,
	})
	Set(l)
	log.SetOutput(l)
	log.SetFlags(0)
}

type logKeyType struct{}

var logKey logKeyType

// FromContext returns the current logger associated with the context,
// the global logger if none is set.
func FromContext(ctx context.Context) Logger {
	if v := ctx.Value(logKey); v != nil {
		return v.(Logger)
	}
	return L()
}

// FromContextFallback returns the current logger associated with the context,
// the fallback if none is set.
func FromContextFallback(ctx context.Context, fallback Logger) Logger {
	if v := ctx.Value(logKey); v != nil {
		return v.(Logger)
	}
	return fallback
}

// With gets the current logger for the context or the global one,
// then constructs a new logger with additional key/value pairs and
// returns a context with that logger stored in it.
func With(ctx context.Context, keysAndValues ...interface{}) context.Context {
	l := FromContext(ctx)
	l = l.With(keysAndValues...)
	return context.WithValue(ctx, logKey, l)
}

// WithLogger returns a context with the given logger stored in it.
func WithLogger(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, logKey, logger)
}
