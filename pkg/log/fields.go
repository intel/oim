/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package log

import (
	"fmt"
)

// Field holds a single key/value pair.
type Field struct {
	Key, Value interface{}
}

func (f Field) String() string {
	return fmt.Sprintf("%s=%s", f.Key, f.Value)
}

// Fields holds key/value pairs for a structured logger.
type Fields []Field

// Clone creates a copy with the current set of fields plus any extra
// ones that might be passed as parameters.
func (f Fields) Clone(keysAndValues ...interface{}) Fields {
	clone := f
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		clone = append(clone, Field{keysAndValues[i], keysAndValues[i+1]})
	}
	return clone
}

// LineBuffer can be used to store a byte slice as a field and convert
// it to a string only on demand. It strips all trailing newlines.
type LineBuffer []byte

func (s LineBuffer) String() string {
	length := len(s)
	for length > 0 && s[length-1] == '\n' {
		length--
	}
	str := string(s[:length])
	return str
}
