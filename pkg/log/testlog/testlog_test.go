/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package testlog

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/intel/oim/pkg/log"
)

func TestGlobal(t *testing.T) {
	old := log.L()
	restore := SetGlobal(t)
	defer restore()
	assert.IsType(t, New(t), log.L())

	restore()
	assert.Exactly(t, old, log.L())
}

func TestOutput1(t *testing.T) {
	defer SetGlobal(t)()

	log.L().Info("TestOutput1")
}

func TestOutput2(t *testing.T) {
	defer SetGlobal(t)()

	log.L().Info("TestOutput2")
	if os.Getenv("TEST_OUTPUT2_FAIL") != "" {
		t.Fatal("was asked to fail")
	}
}

// When adding or removing lines above, update the line numbers in
// the expected output below.

func stripTimes(out []byte) string {
	s := string(out)
	parts := regexp.MustCompile(`\d+\.\d+s`).Split(s, -1)
	return strings.Join(parts, "")
}

var testOutputLogMsg = regexp.MustCompile(`\n\s+([^\n]*\.go:\d+:)`)

func normalizeGoTestOutput(output string) string {
	result := output
	// Indention of test output changed from using tabs to using spaces.
	// Replace with a single tab. Example:
	//   testlog_test.go:40: INFO TestOutput2
	result = testOutputLogMsg.ReplaceAllString(result, `\n $1`)
	return result
}

func TestOutputSilent(t *testing.T) {
	cmd := exec.Command(
		"go", "test",
		"-run", "Output[12]",
		"github.com/intel/oim/pkg/log/testlog",
	)
	cmd.Env = append(os.Environ(), "TEST_OUTPUT2_FAIL=1", "GOCACHE=off")
	out, err := cmd.CombinedOutput()
	assert.Error(t, err)
	assert.Equal(t, normalizeGoTestOutput(`--- FAIL: TestOutput2 ()
	testlog_test.go:40: INFO TestOutput2
	testlog_test.go:42: was asked to fail
FAIL
FAIL	github.com/intel/oim/pkg/log/testlog	
`),
		normalizeGoTestOutput(stripTimes(out)))
}

func TestOutputVerbose(t *testing.T) {
	cmd := exec.Command(
		"go", "test",
		"-v",
		"-run", "Output[12]",
		"github.com/intel/oim/pkg/log/testlog",
	)
	cmd.Env = append(os.Environ(), "TEST_OUTPUT2_FAIL=1", "GOCACHE=off")
	out, err := cmd.CombinedOutput()
	assert.Error(t, err)
	assert.Equal(t, normalizeGoTestOutput(`=== RUN   TestOutput1
--- PASS: TestOutput1 ()
	testlog_test.go:34: INFO TestOutput1
=== RUN   TestOutput2
--- FAIL: TestOutput2 ()
	testlog_test.go:40: INFO TestOutput2
	testlog_test.go:42: was asked to fail
FAIL
FAIL	github.com/intel/oim/pkg/log/testlog	
`),
		normalizeGoTestOutput(stripTimes(out)))
}
