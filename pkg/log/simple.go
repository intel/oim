/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package log

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/intel/oim/pkg/log/level"
)

const (
	logLevelName = "log.level"
)

var logLevel = level.Info
var logOutput = os.Stdout

// InitSimpleFlags sets up flags that configure a simple logger.
// Currently there is only one, -log.level <threshold>.
// Always returns true, which makes it possible to do:
// var _ = InitSimpleFlags()
func InitSimpleFlags() bool {
	if flag.Lookup(logLevelName) == nil {
		valid := make([]string, 0, level.Max+-level.Min)
		for i := level.Min; i <= level.Max; i++ {
			valid = append(valid, i.String())
		}
		flag.Var(&logLevel, logLevelName,
			fmt.Sprintf("log messages at this or a higher level are printed (default: %s, valid: %s)",
				logLevel, strings.Join(valid, "/")))
	}
	return true
}

// NewSimpleConfig returns a configuration for NewSimpleLogger
// that is populated by command line flags. InitSimpleFlags and
// flag.Parse must have been called first, otherwise the defaults
// are returned.
func NewSimpleConfig() SimpleConfig {
	return SimpleConfig{
		Level:  logLevel,
		Output: logOutput,
	}
}

// SimpleConfig contains the configuration for NewSimpleLogger.
type SimpleConfig struct {
	Level  Threshold
	Output io.Writer
}

// NewSimpleLogger constructs a new simple logger.
// The output of the simple logger is plain text (see Formatter).
func NewSimpleLogger(config SimpleConfig) Logger {
	return newSimpleLogger(config)
}

func newSimpleLogger(config SimpleConfig) *simpleLogger {
	logger := &simpleLogger{
		config: config,
	}
	logger.LoggerBase.Init(logger)
	return logger
}

// simpleLogger writes log output at or above a certain threshold
// to an io.writer. In addition, it can also format log messages.
type simpleLogger struct {
	LoggerBase
	config    SimpleConfig
	fields    Fields
	formatter Formatter
}

func (sl *simpleLogger) checkThreshold(threshold Threshold) {
	switch threshold {
	case level.Fatal:
		os.Exit(1)
	case level.Panic:
		panic("fatal error")
	}
}

func (sl *simpleLogger) Output(threshold Threshold, args ...interface{}) {
	if threshold >= sl.config.Level {
		// Errors intentionally ignored here.
		sl.config.Output.Write(sl.formatter.Print(threshold, sl.fields, args...)) // nolint: gosec
	}
	sl.checkThreshold(threshold)
}

func (sl *simpleLogger) Outputf(threshold Threshold, format string, args ...interface{}) {
	if threshold >= sl.config.Level {
		// Errors intentionally ignored here.
		sl.config.Output.Write(sl.formatter.Printf(threshold, sl.fields, format, args...)) // nolint: gosec
	}
	sl.checkThreshold(threshold)
}

func (sl *simpleLogger) Outputw(threshold Threshold, msg string, keysAndValues ...interface{}) {
	if threshold >= sl.config.Level {
		// Errors intentionally ignored here.
		sl.config.Output.Write(sl.formatter.Printw(threshold, sl.fields, msg, keysAndValues...)) // nolint: gosec
	}
	sl.checkThreshold(threshold)
}

// With adds a variadic number of fields to the logging
// context. It accepts loosely-typed key-value pairs. When
// processing pairs, the first element of the pair is used as
// the field key and the second as the field value.
func (sl *simpleLogger) With(keysAndValues ...interface{}) Logger {
	logger := newSimpleLogger(sl.config)
	logger.fields = sl.fields.Clone(keysAndValues...)
	return logger
}

// Write turns each call into a single log message.
func (sl *simpleLogger) Write(p []byte) (n int, err error) {
	// Only convert to string when needed.
	sl.Output(level.Info, LineBuffer(p))
	return len(p), nil
}
