/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcsidriver

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/kubernetes-csi/csi-test/pkg/sanity"
	"github.com/stretchr/testify/require"
)

// SudoMount provides wrappers around several commands used by the k8s
// mount utility code. It then runs those commands under pseudo. This
// allows building and running tests as normal users.
type SudoMount struct {
	tmpDir     string
	searchPath string
}

func SetupSudoMount(t *testing.T) SudoMount {
	tmpDir, err := ioutil.TempDir("", "sanity-node")
	require.NoError(t, err)
	s := SudoMount{
		tmpDir:     tmpDir,
		searchPath: os.Getenv("PATH"),
	}
	for _, cmd := range []string{"mount", "umount", "blkid", "fsck", "mkfs.ext2", "mkfs.ext3", "mkfs.ext4"} {
		wrapper := filepath.Join(s.tmpDir, cmd)
		content := fmt.Sprintf(`#!/bin/sh
PATH=%q
if [ $(id -u) != 0 ]; then
   exec sudo %s "$@"
else
   exec %s "$@"
fi
`, s.searchPath, cmd, cmd)
		err := ioutil.WriteFile(wrapper, []byte(content), 0777)
		require.NoError(t, err)
	}
	os.Setenv("PATH", tmpDir+":"+s.searchPath)
	return s
}

func (s SudoMount) Close() {
	os.RemoveAll(s.tmpDir)
	os.Setenv("PATH", s.searchPath)
}

func TestOIMDriver(t *testing.T) {
	tmp, err := ioutil.TempDir("", "oim-driver")
	require.NoError(t, err)
	defer os.RemoveAll(tmp)

	driver := GetOIMDriver()
	endpoint := "unix://" + tmp + "/oim-driver.sock"
	s, err := driver.Start("oim-driver", "test-node", endpoint)
	defer s.ForceStop()

	sudo := SetupSudoMount(t)
	defer sudo.Close()

	// Now call the test suite.
	config := sanity.Config{
		TargetPath:     tmp + "/target-path",
		StagingPath:    tmp + "/staging-path",
		Address:        endpoint,
		TestVolumeSize: 1 * 1024 * 1024,
	}
	sanity.Test(t, &config)
}
