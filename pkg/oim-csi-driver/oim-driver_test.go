/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/kubernetes-csi/csi-test/pkg/sanity"
	"github.com/stretchr/testify/require"
)

func TestOIMDriver(t *testing.T) {
	tmp, err := ioutil.TempDir("", "oim-driver")
	require.NoError(t, err)
	defer os.RemoveAll(tmp)

	driver := GetOIMDriver()
	endpoint := "unix://" + tmp + "/oim-driver.sock"
	s, err := driver.Start("oim-driver", "test-node", endpoint)
	defer s.ForceStop()

	// Now call the test suite.
	config := sanity.Config{
		TargetPath:  tmp + "/target-path",
		StagingPath: tmp + "/staging-path",
		Address:     endpoint,
	}
	sanity.Test(t, &config)
}
