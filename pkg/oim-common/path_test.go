/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitRegistryPath(t *testing.T) {
	cases := []struct {
		path     string
		elements []string
		err      string
	}{
		{"a/b/c", []string{"a", "b", "c"}, ""},
		{"/a/b//c/", []string{"a", "b", "c"}, ""},
		{".", nil, ".: \".\" not allowed as path element"},
		{"foo/../bar", nil, "foo/../bar: \"..\" not allowed as path element"},
	}

	for _, c := range cases {
		elements, err := SplitRegistryPath(c.path)
		if c.err == "" {
			assert.NoError(t, err)
		} else {
			assert.Equal(t, err.Error(), c.err)
		}
		assert.Equal(t, elements, c.elements)
	}
}
