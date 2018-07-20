/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

// Package level provides a common definition of the severity of log
// or trace events. The advantage compared to numeric log levels as
// used in glog is that the severity is immediately obvious. The
// levels defined here are intentionally the same as in
// uber/zap and logrus.
package level

import (
	"fmt"
	"strings"
)

// Threshold defines the severity of a log or trace event.
// It satisfies the flag.Value interface and thus can be used
// like this:
// var t
// func init() { flag.Var(&t, "threshold", "")
type Threshold int

func (t Threshold) String() string {
	return prefixes[t]
}

// Set expects the name of a threshold level (like INFO), ignoring the case.
func (t *Threshold) Set(value string) error {
	upper := strings.ToUpper(value)
	for i := Min; i <= Max; i++ {
		if upper == i.String() {
			*t = i
			return nil
		}
	}
	return fmt.Errorf("invalid threshold: %s", value)
}

const (
	// Debug = relevant only for a developer during debugging.
	Debug Threshold = iota
	// Info = informational, may be useful also for users.
	Info
	// Warn = warning, may or may not be something that is worth investigating.
	Warn
	// Error = should not occur during normal operation, but the program can continue.
	Error
	// Fatal = an error that is so severe that the program needs to quit.
	Fatal
	// Panic = an error that is so severe that the program needs to abort.
	Panic

	// Min is the smallest threshold.
	Min = Debug

	// Max is the largest threshold.
	Max = Panic
)

var prefixes = []string{
	"DEBUG",
	"INFO",
	"WARN",
	"ERROR",
	"FATAL",
	"PANIC",
}
