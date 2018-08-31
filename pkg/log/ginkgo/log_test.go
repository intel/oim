/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package ginkgo_test

import (
	"os"
	"os/exec"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/intel/oim/pkg/log"
)

var _ = Describe("Log", func() {
	BeforeEach(func() {
		log.L().Info("in BeforeEach via log.L.Info")
	})

	It("output1", func() {
		log.L().Info("in output1 via log.L().Info")
		GinkgoWriter.Write([]byte("in output1 via GinkgoWriter\n"))
	})

	It("output2", func() {
		log.L().Info("in output2 via log.L().Info")
		GinkgoWriter.Write([]byte("in output2 via GinkgoWriter\n"))

		if os.Getenv("TEST_BAR_FAIL") != "" {
			Fail("was asked to fail")
		}
	})

	It("hides output", func() {
		cmd := exec.Command(
			"go", "test",
			"github.com/intel/oim/pkg/log/ginkgo",
			"-args", "-ginkgo.focus=output[12]", "-ginkgo.noColor",
		)
		cmd.Env = append(os.Environ(), "TEST_BAR_FAIL=1", "GOCACHE=off")
		out, err := cmd.CombinedOutput()
		Expect(err).To(HaveOccurred())
		Expect(stripVars(out)).To(Equal(`Running Suite: Ginkgo Suite
===========================
xxx
Will run 2 of 3 specs

•INFO in BeforeEach via log.L.Info
INFO in output2 via log.L().Info
in output2 via GinkgoWriter

------------------------------
• Failure [xxx]
Log
xxx/pkg/log/ginkgo/log_test.go:21
  output2 [It]
  xxx/pkg/log/ginkgo/log_test.go:31

  was asked to fail

  xxx/pkg/log/ginkgo/log_test.go:36
------------------------------
S

Summarizing 1 Failure:

[Fail] Log [It] output2 
xxx/pkg/log/ginkgo/log_test.go:36

Ran 2 of 3 Specs in xxx
FAIL! -- 1 Passed | 1 Failed | 0 Pending | 1 Skipped
--- FAIL: TestGinkgo (xxx)
FAIL
FAIL	github.com/intel/oim/pkg/log/ginkgo	xxx
`))
	})
})

func stripVars(out []byte) string {
	s := string(out)
	parts := regexp.MustCompile(`\d+\.\d+(s|ms|m| seconds)|Random Seed: \d+|/[^\n]*/intel/oim`).Split(s, -1)
	return strings.Join(parts, "xxx")
}
