/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"bufio"
	"io"
	"sync"

	"github.com/intel/oim/pkg/log"
)

// LogWriter returns a WriteCloser that logs individual
// lines as they get written through the logger.
func LogWriter(logger log.Logger) io.WriteCloser {
	pr, pw := io.Pipe()
	scanner := bufio.NewScanner(pr)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for scanner.Scan() {
			// When used with testing.T, the source
			// location of this call will be printed. This
			// cannot be avoided because there isn't
			// really any better location in the call
			// stack (it is its own goroutine).
			logger.Debugf("%s", log.LineBuffer(scanner.Bytes()))
		}
	}()
	return &logWriterCloser{pw, wg}
}

type logWriterCloser struct {
	io.WriteCloser
	wg *sync.WaitGroup
}

func (c *logWriterCloser) Close() error {
	err := c.WriteCloser.Close()
	c.wg.Wait()
	return err
}
