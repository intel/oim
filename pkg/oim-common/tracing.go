/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018 Intel Corporation.

SPDX-License-Identifier: Apache-2.0
*/

package oimcommon

import (
	"context"
	"fmt"
	"io"

	"google.golang.org/grpc"

	// TODO: re-enable tracing once https://github.com/jaegertracing/jaeger-lib/issues/32 is addressed.
	// "github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	// "github.com/opentracing/opentracing-go"
	// otlog "github.com/opentracing/opentracing-go/log"
	// jaegercfg "github.com/uber/jaeger-client-go/config"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/intel/oim/pkg/log"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
)

// PayloadFormatter is responsible for turning a gRPC request or response
// into a string.
type PayloadFormatter interface {
	// Sprint serializes the gRPC request or response as string.
	Sprint(payload interface{}) string
}

// CompletePayloadFormatter dumps the entire request or response as
// string. Beware that this may include sensitive information!
type CompletePayloadFormatter struct{}

// Sprint uses fmt.Sprint("%+v") to format the entire payload.
func (c CompletePayloadFormatter) Sprint(payload interface{}) string {
	result := fmt.Sprintf("%+v", payload)
	if result == "" {
		// Seeing "response:" in a gRPC trace is confusing.
		// Show something instead that confirms that really
		// nothing was sent or received.
		return "<empty>"
	}
	return result
}

// StripSecretsFormatter removes secret fields from a CSI 0.3 or CSI 1.0 message
// using the protosanitizer package.
type StripSecretsFormatter struct{}

// This is a compile-time check that we are still using CSI 0.3 and thus
// have to use StripSecretsCSI03. It'll fail when switching to CSI 1.0,
// in which case we must switch to filtering with StripSecrets.
var _ = csi.CreateVolumeRequest{
	ControllerCreateSecrets: map[string]string{},
}

// Sprint currently strips messages for CSI 0.3. It needs to be updated
// when migrating to CSI 1.0.
func (s StripSecretsFormatter) Sprint(payload interface{}) string {
	return protosanitizer.StripSecretsCSI03(payload).String()
}

// NullPayloadFormatter just produces "nil" or "<filtered>".
type NullPayloadFormatter struct{}

// Sprint just produces "nil" or "<filtered>".
func (n NullPayloadFormatter) Sprint(payload interface{}) string {
	if payload == nil {
		return "nil"
	}
	return "<filtered>"
}

// delayedFormatter takes a formatter and a payload and
// formats as string when needed.
type delayedFormatter struct {
	formatter PayloadFormatter
	payload   interface{}
}

func (d *delayedFormatter) String() string {
	return d.formatter.Sprint(d.payload)
}

// LogGRPCServer returns a gRPC interceptor for a gRPC server which
// logs the server-side call information via the provided logger.
// Method names are printed at the "Debug" level, with detailed
// request and response information if (and only if!) a formatter for
// those is provided. That's because sensitive information may
// be included in those data structures. Failed method calls
// are printed at the "Error" level.
//
// If this interceptor is invoked after the otgrpc.OpenTracingServerInterceptor,
// then it will install a logger which adds log events to the span in
// addition to passing them on to the original logger.
func LogGRPCServer(logger log.Logger, formatter PayloadFormatter) grpc.UnaryServerInterceptor {
	if formatter == nil {
		// Always print some information about the payload.
		formatter = NullPayloadFormatter{}
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ctx = logGRPCPre(ctx, logger, formatter, "received", info.FullMethod, req)
		innerCtx := ctx
		// if sp := opentracing.SpanFromContext(ctx); sp != nil {
		// 	l := log.FromContext(ctx)
		// 	l = newSpanLogger(sp, l)
		// 	innerCtx = log.WithLogger(ctx, l)
		// }
		resp, err := handler(innerCtx, req)
		logGRPCPost(ctx, formatter, "sending", err, resp)
		return resp, err
	}
}

// LogGRPCClient does the same as LogGRPCServer, only on the client side.
// There is no need for a logger because that gets passed in.
func LogGRPCClient(formatter PayloadFormatter) grpc.UnaryClientInterceptor {
	if formatter == nil {
		// Always print some information about the payload.
		formatter = NullPayloadFormatter{}
	}

	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = logGRPCPre(ctx, log.FromContext(ctx), formatter, "sending", method, req)
		innerCtx := ctx
		// if sp := opentracing.SpanFromContext(ctx); sp != nil {
		// 	logger := log.FromContext(ctx)
		// 	logger = newSpanLogger(sp, logger)
		// 	innerCtx = log.WithLogger(ctx, logger)
		// }
		err := invoker(innerCtx, method, req, reply, cc, opts...)
		logGRPCPost(ctx, formatter, "received", err, reply)
		return err
	}
}

func logGRPCPre(ctx context.Context, logger log.Logger, formatter PayloadFormatter, msg, method string, req interface{}) context.Context {
	// Determine indention level based on context and increment it by
	// by one for future logging.
	logger = logger.With("method", method)
	logger.Debugw(msg, "request", &delayedFormatter{formatter, req})
	return log.WithLogger(ctx, logger)
}

func logGRPCPost(ctx context.Context, formatter PayloadFormatter, msg string, err error, reply interface{}) {
	if err != nil {
		log.FromContext(ctx).Errorw(msg, "error", err)
	} else {
		log.FromContext(ctx).Debugw(msg, "response", &delayedFormatter{formatter, reply})
	}
}

// TraceGRPCPayload returns a span decorator which adds the request
// and response as tags to the call's span if (and only if) a
// formatter is given.
// func TraceGRPCPayload(formatter PayloadFormatter) otgrpc.SpanDecoratorFunc {
// 	return func(sp opentracing.Span, method string, req, reply interface{}, err error) {
// 		if formatter != nil {
// 			sp.SetTag("request", &delayedFormatter{formatter, req})
// 			if err == nil {
// 				sp.SetTag("response", &delayedFormatter{formatter, reply})
// 			}
// 		}
// 	}
// }

// type spanLogger struct {
// 	log.LoggerBase
// 	sp     opentracing.Span
// 	logger log.Logger
// }

// func newSpanLogger(sp opentracing.Span, logger log.Logger) log.Logger {
// 	l := &spanLogger{sp: sp, logger: logger}
// 	l.LoggerBase.Init(l)
// 	return l
// }

// func (sl *spanLogger) Output(threshold log.Threshold, args ...interface{}) {
// 	sl.sp.LogFields(otlog.Lazy(func(fv otlog.Encoder) {
// 		fv.EmitString("event", strings.ToLower(threshold.String()))
// 		fv.EmitString("message", fmt.Sprint(args...))
// 	}))
// 	sl.logger.Output(threshold, args...)
// }

// func (sl *spanLogger) Outputf(threshold log.Threshold, format string, args ...interface{}) {
// 	sl.sp.LogFields(otlog.Lazy(func(fv otlog.Encoder) {
// 		fv.EmitString("event", strings.ToLower(threshold.String()))
// 		fv.EmitString("message", fmt.Sprintf(format, args...))
// 	}))
// 	sl.logger.Outputf(threshold, format, args...)
// }

// func (sl *spanLogger) Outputw(threshold log.Threshold, msg string, keysAndValues ...interface{}) {
// 	sl.sp.LogFields(otlog.Lazy(func(fv otlog.Encoder) {
// 		fv.EmitString("event", strings.ToLower(threshold.String()))
// 		fv.EmitString("message", msg)
// 		for i := 0; i+1 < len(keysAndValues); i += 2 {
// 			// We rely in reflection inside emitObject
// 			// here instead of trying to switch by the
// 			// type of the value ourselves, like
// 			// otlog.InterleavedKVToFields does.
// 			fv.EmitObject(fmt.Sprintf("%s", keysAndValues[i]),
// 				keysAndValues[i+1])
// 		}
// 	}))
// 	sl.logger.Outputw(threshold, msg, keysAndValues...)
// }

// // With creates a new instance with the same span and a logger
// // which has the additional fields added.
// func (sl *spanLogger) With(keysAndValues ...interface{}) log.Logger {
// 	return newSpanLogger(sl.sp,
// 		sl.logger.With(keysAndValues...))
// }

type nopCloser struct{}

func (n nopCloser) Close() error { return nil }

// InitTracer initializes the global OpenTracing tracer, using Jaeger
// and the provided name for the current process. Must be called at
// the start of main(). The result is a function which should be
// called at the end of main() to clean up.
func InitTracer(component string) (io.Closer, error) {
	// // Add support for the usual env variables, in particular
	// // JAEGER_AGENT_HOST, which is needed when running only one
	// // Jaeger agent per cluster.
	// cfg, err := jaegercfg.FromEnv()
	// if err != nil {
	// 	// parsing errors might happen here, such as when we get a string where we expect a number
	// 	return nil, err
	// }

	// // Initialize tracer singleton.
	// closer, err = cfg.InitGlobalTracer(component)
	closer := nopCloser{}
	return closer, nil
}
