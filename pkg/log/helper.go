/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package log

import (
	"github.com/intel/oim/pkg/log/level"
)

// The LoggerBase struct can be embedded to simplify the implementation
// of a Logger: the implementer then only has to implement the
// three output functions plus With.
type LoggerBase struct {
	// Print logs a message the same way as the function with the
	// same name as the threshold, using fmt.Sprint, then
	// exits or panics if needed.
	Print func(threshold Threshold, args ...interface{})
	// Printf logs a message the same way as the function with the
	// same name as the threshold, using fmt.Sprintf, then exits
	// or panics if needed.
	Printf func(threshold Threshold, format string, args ...interface{})
	// Printw logs a message the same way as the function with the
	// same name as the threshold, using the same key-value pairs
	// as for With, then exits or panics if needed.
	Printw func(threshold Threshold, msg string, keysAndValues ...interface{})
}

// Init installs pointers to the three output functions in the
// LoggerBase.
func (lb *LoggerBase) Init(logger Logger) {
	lb.Print = logger.Output
	lb.Printf = logger.Outputf
	lb.Printw = logger.Outputw
}

// Debug uses fmt.Sprint to construct and log a message.
func (lb *LoggerBase) Debug(args ...interface{}) {
	lb.Print(level.Debug, args...)
}

// Debugf uses fmt.Sprintf to log a templated message.
func (lb *LoggerBase) Debugf(format string, args ...interface{}) {
	lb.Printf(level.Debug, format, args...)
}

// Debugw logs a message with some additional context. The
// variadic key-value pairs are treated as they are in With.
func (lb *LoggerBase) Debugw(msg string, keysAndValues ...interface{}) {
	lb.Printw(level.Debug, msg, keysAndValues...)
}

// Info uses fmt.Sprint to construct and log a message.
func (lb *LoggerBase) Info(args ...interface{}) {
	lb.Print(level.Info, args...)
}

// Infof uses fmt.Sprintf to log a templated message.
func (lb *LoggerBase) Infof(format string, args ...interface{}) {
	lb.Printf(level.Info, format, args...)
}

// Infow logs a message with some additional context. The
// variadic key-value pairs are treated as they are in With.
func (lb *LoggerBase) Infow(msg string, keysAndValues ...interface{}) {
	lb.Printw(level.Info, msg, keysAndValues...)
}

// Warn uses fmt.Sprint to construct and log a message.
func (lb *LoggerBase) Warn(args ...interface{}) {
	lb.Print(level.Warn, args...)
}

// Warnf uses fmt.Sprintf to log a templated message.
func (lb *LoggerBase) Warnf(format string, args ...interface{}) {
	lb.Printf(level.Warn, format, args...)
}

// Warnw logs a message with some additional context. The
// variadic key-value pairs are treated as they are in With.
func (lb *LoggerBase) Warnw(msg string, keysAndValues ...interface{}) {
	lb.Printw(level.Warn, msg, keysAndValues...)
}

// Error uses fmt.Sprint to construct and log a message.
func (lb *LoggerBase) Error(args ...interface{}) {
	lb.Print(level.Error, args...)
}

// Errorf uses fmt.Sprintf to log a templated message.
func (lb *LoggerBase) Errorf(format string, args ...interface{}) {
	lb.Printf(level.Error, format, args...)
}

// Errorw logs a message with some additional context. The
// variadic key-value pairs are treated as they are in With.
func (lb *LoggerBase) Errorw(msg string, keysAndValues ...interface{}) {
	lb.Printw(level.Error, msg, keysAndValues...)
}

// Fatal uses fmt.Sprint to construct and log a message.
func (lb *LoggerBase) Fatal(args ...interface{}) {
	lb.Print(level.Fatal, args...)
}

// Fatalf uses fmt.Sprintf to log a templated message.
func (lb *LoggerBase) Fatalf(format string, args ...interface{}) {
	lb.Printf(level.Fatal, format, args...)
}

// Fatalw logs a message with some additional context. The
// variadic key-value pairs are treated as they are in With.
func (lb *LoggerBase) Fatalw(msg string, keysAndValues ...interface{}) {
	lb.Printw(level.Fatal, msg, keysAndValues...)
}

// Panic uses fmt.Sprint to construct and log a message.
func (lb *LoggerBase) Panic(args ...interface{}) {
	lb.Print(level.Panic, args...)
}

// Panicf uses fmt.Sprintf to log a templated message.
func (lb *LoggerBase) Panicf(format string, args ...interface{}) {
	lb.Printf(level.Panic, format, args...)
}

// Panicw logs a message with some additional context. The
// variadic key-value pairs are treated as they are in With.
func (lb *LoggerBase) Panicw(msg string, keysAndValues ...interface{}) {
	lb.Printw(level.Panic, msg, keysAndValues...)
}
