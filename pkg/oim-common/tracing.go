/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"fmt"
	"io"
	"strings"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	jaegercfg "github.com/uber/jaeger-client-go/config"
)

type logIndentKeyType struct{}

var logIndentKey logIndentKeyType

// logIndentIncrement specifies the number of spaces that log messages
// get indented while handling gRPC calls.
var logIndentIncrement = 2

// logIndent returns the current indention associated with a context,
// zero if none.
func logIndent(ctx context.Context) int {
	if v := ctx.Value(logIndentKey); v != nil {
		return v.(int)
	}
	return 0
}

// logSpaces returns a string containing enough spaces for the
// current indent.
func logSpaces(ctx context.Context) string {
	indent := logIndent(ctx)
	return strings.Repeat(" ", indent)
}

// LogGRPCServer logs the server-side call information via glog.
//
// Warning: at log levels >= 5 the recorded information includes all
// parameters, which potentially contains sensitive information like
// the secrets.
func LogGRPCServer(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	innerCtx, indent := logGRPCPre(ctx, info.FullMethod, req)
	resp, err := handler(innerCtx, req)
	logGRPCPost(indent, info.FullMethod, err, resp)
	return resp, err
}

// LogGRPCClient does the same as LogGRPCServer, only on the client side.
// TODO: indent nested calls
func LogGRPCClient(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	innerCtx, indent := logGRPCPre(ctx, method, req)
	err := invoker(innerCtx, method, req, reply, cc, opts...)
	logGRPCPost(indent, method, err, reply)
	return err
}

func logGRPCPre(ctx context.Context, method string, req interface{}) (context.Context, int) {
	// Determine indention level based on context and increment it by
	// by one for future logging.
	indent := logIndent(ctx)
	if glog.V(5) {
		glog.Infof("%*sGRPC call: %s %s", indent, "", method, req)
	} else if glog.V(3) {
		glog.Infof("%*sGRPC call: %s", indent, "", method)
	}
	return context.WithValue(ctx, logIndentKey, indent+logIndentIncrement), indent
}

func logGRPCPost(indent int, method string, err error, reply interface{}) {
	// We need to include the method name here because
	// a) the preamble might have been logged quite a while
	//    ago with more log entries since then
	// b) logging of the preamble might have been skipped due
	//    to the lower log level
	if err != nil {
		glog.Errorf("%*sGRPC error: %s: %v", indent, "", method, err)
	} else {
		glog.V(5).Infof("%*sGRPC response: %s: %+v", indent, "", method, reply)
	}
}

// TraceGRPCPayload adds the request and response as tags
// to the call's span, if the log level is five or higher.
// Warning: this may include sensitive information like the
// secrets.
func TraceGRPCPayload(sp opentracing.Span, method string, req, reply interface{}, err error) {
	if glog.V(5) {
		sp.SetTag("request", req)
		if err == nil {
			sp.SetTag("response", reply)
		}
	}
}

// Infof logs with glog.V(level).Infof() and in addition, always adds
// a log message to the current tracing span if the context has
// one. This ensures that spans which get recorded (not all do) have
// the full information.
func Infof(level glog.Level, ctx context.Context, format string, args ...interface{}) {
	if glog.V(level) {
		glog.InfoDepth(1, fmt.Sprintf(logSpaces(ctx)+format, args...))
	}
	sp := opentracing.SpanFromContext(ctx)
	if sp != nil {
		sp.LogFields(otlog.Lazy(func(fv otlog.Encoder) {
			fv.EmitString("message", fmt.Sprintf(format, args...))
		}))
	}
}

// Errorf does the same as Infof for error messages, except that
// it ignores the current log level.
func Errorf(ctx context.Context, format string, args ...interface{}) {
	glog.ErrorDepth(1, fmt.Sprintf(logSpaces(ctx)+format, args...))
	sp := opentracing.SpanFromContext(ctx)
	if sp != nil {
		sp.LogFields(otlog.Lazy(func(fv otlog.Encoder) {
			fv.EmitString("event", "error")
			fv.EmitString("message", fmt.Sprintf(format, args...))
		}))
	}
}

// InitTracer initializes the global OpenTracing tracer, using Jaeger
// and the provided name for the current process. Must be called at
// the start of main(). The result is a function which should be
// called at the end of main() to clean up.
func InitTracer(component string) (closer io.Closer, err error) {
	// Add support for the usual env variables, in particular
	// JAEGER_AGENT_HOST, which is needed when running only one
	// Jaeger agent per cluster.
	cfg, err := jaegercfg.FromEnv()
	if err != nil {
		// parsing errors might happen here, such as when we get a string where we expect a number
		return
	}

	// Initialize tracer singleton.
	closer, err = cfg.InitGlobalTracer(component)
	return
}
