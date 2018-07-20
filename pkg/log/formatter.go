/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package log

import (
	"bytes"
	"fmt"
)

// Formatter can format fields together with a message and severity
// threshold as plain text. Currently it cannot be customized.
// The ordering of fields is:
// <time> <level> [<at>: ]<message> [<key>: <value>]
type Formatter struct{}

// Print formats using fmt.Sprint. It adds a line break at the end.
func (f *Formatter) Print(threshold Threshold, fields Fields, args ...interface{}) []byte {
	return f.Printw(threshold, fields, fmt.Sprint(args...))
}

// Printf formats using fmt.Sprintf. It adds a line break at the end.
func (f *Formatter) Printf(threshold Threshold, fields Fields, format string, args ...interface{}) []byte {
	return f.Printw(threshold, fields, fmt.Sprintf(format, args...))
}

// Printw formats a string message with additional key/value pairs. It
// adds a line break at the end.
func (f *Formatter) Printw(threshold Threshold, fields Fields, msg string, keysAndValues ...interface{}) []byte {
	// Find special fields and convert to bytes.
	all := fields.Clone(keysAndValues...)
	additional := make([]Field, 0, len(all))
	var time []byte
	level := []byte(threshold.String())
	var at [][]byte

	for _, field := range all {
		key := fmt.Sprintf("%s", field.Key)
		value := []byte(fmt.Sprintf("%s", field.Value))
		switch key {
		case "time":
			time = value
		case "at":
			// We allow more than one "at" entry.
			at = append(at, value)
		case "level":
			// The threshold sets the default, but it might still get overwritten.
			level = value
		default:
			additional = append(additional, Field{[]byte(key), []byte(value)})
		}
	}

	// Four fixed fields, separator, newline and additional key/value pairs.
	s := make([][]byte, 0, 6+2*len(additional))
	if len(time) > 0 {
		s = append(s, time)
	}
	s = append(s, level)
	if len(at) > 0 {
		s = append(s, append(bytes.Join(at, []byte{'/'}), ':'))
	}
	if msg != "" {
		s = append(s, []byte(msg))
		if len(additional) > 0 {
			s = append(s, []byte{'|'})
		}
	}
	for _, field := range additional {
		s = append(s, append(field.Key.([]byte), ':'))
		s = append(s, field.Value.([]byte))
	}

	// Separated by space, followed by newline.
	line := bytes.Join(s, []byte{' '})
	line = append(line, '\n')

	return line
}
