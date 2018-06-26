/*
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"sync"
)

// SimpleLogger is the interface for the logging calls in testing.T.
//
// This interface is useful for code which needs to do proper test
// logging when called from Ginkgo and from the normal testing
// framework: with Gingko, the normal log package can be used
// and the output will be associated with the current test (not printed
// in silent mode, dumped when a failure occurs). With testing.T
// that only works when using the testing.T.Log methods.
type SimpleLogger interface {
	Log(args ...interface{})
	Logf(format string, args ...interface{})
}

// DiscardLogger ignores all log output.
type DiscardLogger struct{}

func (dl DiscardLogger) Log(args ...interface{}) {
}

func (dl DiscardLogger) Logf(format string, args ...interface{}) {
}

// WrapLogger wraps a log.Logger such that it
// implements the SimpleLogger interface.
type WrapLogger struct {
	*log.Logger
}

func (wl WrapLogger) Log(args ...interface{}) {
	wl.Logger.Output(2, fmt.Sprint(args...))
}

func (wl WrapLogger) Logf(format string, args ...interface{}) {
	wl.Logger.Output(2, fmt.Sprintf(format, args...))
}

// WrapWriter wraps a io.Writer such that it implements the
// SimpleLogger interface.
func WrapWriter(writer io.Writer) SimpleLogger {
	return WrapLogger{log.New(writer, "", log.LstdFlags)}
}

// LogWriter returns a WriteCloser that logs individual
// lines as they get written with a prefix. It can wrap
// both a log.Logger as well as a testing.T.
func LogWriter(logger SimpleLogger, prefix string) io.WriteCloser {
	pr, pw := io.Pipe()
	scanner := bufio.NewScanner(pr)
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for scanner.Scan() {
			// When used with testing.T, the source location
			// of this call will be printed. This cannot be
			// avoided because testing.T.Log always prints
			// its direct caller.
			logger.Log(prefix, scanner.Text())
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
