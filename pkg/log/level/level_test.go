/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package level

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLevels(t *testing.T) {
	for i := Debug; i <= Fatal; i++ {
		assert.NotEmpty(t, fmt.Sprintf("%s", i))
	}
	assert.Equal(t, "FATAL", fmt.Sprintf("%s", Fatal))
}

func TestSetLevel(t *testing.T) {
	var threshold Threshold
	var err error

	err = threshold.Set("fatal")
	assert.NoError(t, err)
	assert.Equal(t, Fatal, threshold)

	err = threshold.Set("Info")
	assert.NoError(t, err)
	assert.Equal(t, Info, threshold)

	err = threshold.Set("Foobar")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Foobar")
	assert.Equal(t, Info, threshold)
}
