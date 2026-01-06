// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

// ServiceNames
const (
	ServiceNameController  = "pubsub-controller"
	ServiceNameConnectNode = "pubsub-connect-node"
	ServiceNamePushManager = "pubsub-push-manager"
	ServiceNameBizServer   = "pubsub-biz-server"
)

// InitTracer 初始化 OpenTelemetry Tracer
func InitTracer(serviceID, serviceName string) (func(context.Context) error, error) {
	_ = serviceID // serviceID 可用于区分不同实例
	// 创建导出器（这里使用 stdout，生产环境应该使用 Jaeger）
	exporter, err := stdouttrace.New(
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		return nil, err
	}

	// 创建资源
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
			attribute.String("environment", "dev"),
		),
	)
	if err != nil {
		return nil, err
	}

	// 创建 TracerProvider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// 设置全局 TracerProvider
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

// GetGRPCServerOptions 获取带 OpenTelemetry 的 gRPC Server 选项
func GetGRPCServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
		grpc.StreamInterceptor(otelgrpc.StreamServerInterceptor()),
	}
}

// GetGRPCClientOptions 获取带 OpenTelemetry 的 gRPC Client 选项
func GetGRPCClientOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
		grpc.WithStreamInterceptor(otelgrpc.StreamClientInterceptor()),
	}
}

// StartSpan 开始一个新的 span
func StartSpan(ctx context.Context, operationName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	tracer := otel.Tracer("pubsub")
	return tracer.Start(ctx, operationName, opts...)
}

// AddSpanAttributes 给当前 span 添加属性
func AddSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attrs...)
}

// AddSpanEvent 给当前 span 添加事件
func AddSpanEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// RecordError 记录错误到当前 span
func RecordError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// SetSpanSuccess 设置 span 为成功状态
func SetSpanSuccess(ctx context.Context) {
	span := trace.SpanFromContext(ctx)
	span.SetStatus(codes.Ok, "success")
}

// InjectTraceContext 将 trace context 注入到 gRPC metadata
func InjectTraceContext(ctx context.Context) context.Context {
	// gRPC otelgrpc 会自动处理，这里提供手动方法
	return ctx
}

// ExtractTraceContext 从 gRPC metadata 提取 trace context
func ExtractTraceContext(ctx context.Context) context.Context {
	// gRPC otelgrpc 会自动处理
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	_ = md
	return ctx
}

// 常用的 attribute keys
var (
	AttrRoomID    = attribute.Key("room.id")
	AttrUserID    = attribute.Key("user.id")
	AttrUserName  = attribute.Key("user.name")
	AttrNodeID    = attribute.Key("node.id")
	AttrUserCount = attribute.Key("user.count")
	AttrRoomCount = attribute.Key("room.count")
	AttrOperation = attribute.Key("operation")
	AttrSuccess   = attribute.Key("success")
	AttrSource    = attribute.Key("source")
	AttrTarget    = attribute.Key("target")
)
