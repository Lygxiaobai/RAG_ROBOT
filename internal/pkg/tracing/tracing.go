package tracing

import (
	"context"
	"io"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Tracer 全局 Tracer，各 package 通过 tracing.Tracer 创建子 Span
var Tracer trace.Tracer

// Config 链路追踪配置，对应 config.TracingConfig
type Config struct {
	Enabled     bool
	ServiceName string
	// Exporter: "stdout"（开发）| "jaeger"（生产，需配合 OTLP）
	Exporter string
}

// Init 根据配置初始化 TracerProvider。
//
//   - enabled=false：注册 noop Tracer，所有调用变成空操作，零开销
//   - exporter=stdout：将 Span 以 JSON 格式打印到 stderr，开发调试用
//
// 返回的 shutdown 必须在进程退出前调用（defer），确保 Span 全部 flush。
func Init(cfg Config) (shutdown func(context.Context) error, err error) {
	// 未启用追踪：注册 noop provider，代码路径不变但完全无开销  这是一个空实现 后续的handler/service不用判断是否开启Tracer
	if !cfg.Enabled {
		otel.SetTracerProvider(noop.NewTracerProvider())
		Tracer = noop.NewTracerProvider().Tracer(cfg.ServiceName)
		return func(context.Context) error { return nil }, nil
	}

	//创建输出方式
	exporter, err := buildExporter(cfg.Exporter)
	if err != nil {
		return nil, err
	}

	//给所有的span打上一个标签 它属于哪个服务
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, err
	}

	//创建一个Tracer工厂
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter), // 异步批量发送，不阻塞业务
		sdktrace.WithResource(res),
		//每个请求都采样 全部记录
		sdktrace.WithSampler(sdktrace.AlwaysSample()), // 开发全量采样；生产可改 TraceIDRatioBased(0.1)
	)

	//为otel创建一个默认全局的Tracer工厂，否则在router调用会找不到这边设定好的Tracer工厂
	otel.SetTracerProvider(tp)

	//设置传播器
	//规定Tracer怎么跨服务传播
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		/*
			作用是：
			上游服务传来 traceparent
			你的服务能接着同一个 trace 继续走
			不会自己另起一条孤立链路
		*/
		propagation.TraceContext{}, // W3C traceparent header
		//附加的上下文传播机制
		propagation.Baggage{},
	))

	//为Tracer工厂创建一个工具
	//这个工具用于Span的创建
	Tracer = tp.Tracer(cfg.ServiceName)
	return tp.Shutdown, nil
}

// buildExporter 根据 exporter 名称创建对应的 SpanExporter。
// 目前支持 stdout；后续扩展 jaeger 只需在此加 case。
func buildExporter(name string) (sdktrace.SpanExporter, error) {
	switch name {
	case "jaeger":
		// 生产环境：通过环境变量 OTEL_EXPORTER_OTLP_ENDPOINT 指定 Jaeger/Tempo 地址
		// 需要额外引入 go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc
		// 此处暂时 fallthrough 到 stdout，避免引入未使用的依赖
		fallthrough
	default: // "stdout" 及其他未知值均使用 stdout
		return stdouttrace.New(
			stdouttrace.WithWriter(io.Discard), // 默认丢弃，避免日志污染
			// 开发时可改为 os.Stderr 以在终端查看 Span
			stdouttrace.WithWriter(os.Stderr),
			stdouttrace.WithPrettyPrint(),
		)
	}
}
