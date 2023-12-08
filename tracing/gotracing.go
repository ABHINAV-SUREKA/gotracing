package tracing

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/credentials"
)

var (
	// GRPCTracingEndpoint - the endpoint to send traces to.
	GRPCTracingEndpoint string

	// DefaultSampler - default sampling strategy.
	// If a span doesn't have a parent, turn on sampling.
	// Otherwise, turn on sampling only if the parent is being sampled.
	DefaultSampler = sdktrace.ParentBased(sdktrace.AlwaysSample())

	// DefaultBatchTimeout - max duration for constructing a batch.
	// Processor forcefully sends available spans when timeout is reached (default: 5000 ms).
	DefaultBatchTimeout = sdktrace.DefaultScheduleDelay * time.Millisecond
)

func init() {
	// If NODE_IP isn't set, use "localhost:4317" for GRPCTracingEndpoint
	host := "localhost"
	nodeIp, ok := os.LookupEnv("NODE_IP")
	if ok {
		host = nodeIp
	}
	GRPCTracingEndpoint = net.JoinHostPort(host, "4317")
}

type Manager struct {
	TracerProvider *sdktrace.TracerProvider
	Processor      sdktrace.SpanProcessor
	Propagator     propagation.TextMapPropagator
}

type Config struct {
	// Endpoint to send traces to.
	// Eg: localhost:4317
	// If empty, this will be set to GRPCTracingEndpoint
	Endpoint string

	// Whether to disable client transport security (i.e. not use TLS credentials)
	// for the exporter's gRPC connection to the server.
	Insecure bool

	// Identifying information/metadata about the thing sending the traces.
	// A list of common attributes can be found here.
	//
	// https://opentelemetry.io/docs/specs/semconv/resource/#semantic-attributes-with-sdk-provided-default-value
	Attributes map[string]string

	// If nil, defaults to DefaultSampler
	// Eg: sdktrace.AlwaysSample()
	Sampler sdktrace.Sampler

	BatchTimeout time.Duration

	// If DebugOutput is non-nil, Endpoint will be ignored and trace output will
	// instead be written to the io.Writer.
	DebugOutput io.Writer
}

func New(ctx context.Context, cfg Config) (*Manager, error) {
	log.Infof("Initializing Tracer Provider for endpoint: %s...", cfg.Endpoint)

	if cfg.Endpoint == "" {
		cfg.Endpoint = GRPCTracingEndpoint
	}
	if cfg.Sampler == nil {
		cfg.Sampler = DefaultSampler
	}
	if cfg.BatchTimeout <= 0 {
		cfg.BatchTimeout = DefaultBatchTimeout
	}

	/* Create either an OTLP gRPC Trace Exporter for sending traces to a collector/remote backend/etc.
	OR Stdout Trace Exporter for writing traces to std output
	*/
	var exporter sdktrace.SpanExporter
	var err error
	if cfg.DebugOutput == nil {
		secureOption := otlptracegrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, ""))
		if cfg.Insecure {
			secureOption = otlptracegrpc.WithInsecure()
		}
		traceClient := otlptracegrpc.NewClient(secureOption, otlptracegrpc.WithEndpoint(cfg.Endpoint))
		exporter, err = otlptrace.New(context.Background(), traceClient)
	} else {
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint(), stdouttrace.WithWriter(cfg.DebugOutput))
	}
	if err != nil {
		return nil, fmt.Errorf("could not create trace exporter for Tracer Provider: %s", err)
	}

	/* Define the resources describing the object that generated the telemetry signals.
	 */
	attrs := make([]attribute.KeyValue, len(cfg.Attributes))
	i := 0
	for k, v := range cfg.Attributes {
		attrs[i] = attribute.String(k, v)
		i++
	}

	// Eg:
	//resources, err := resource.New(ctx,
	//	resource.WithAttributes(
	//		attribute.String("service.name", serviceName),
	//		attribute.String("service.namespace", "infra-o11y-seg"),
	//		attribute.String("library.language", "go"),
	//	),
	//)
	resources, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return nil, err
	}

	/* Create TracerProvider.
	TracerProvider is a factory for creating & configuring tracers.
	Each tracer traces/records info about a single operation or request as it traverses different parts of a distributed system.
	TraceProvider then samples these traces and send them in batches to the collector/endpoint via the Exporter.
	*/

	// Note: BatchSpanProcessor processes spans in batches before they are exported. Preferred processor.
	// SimpleSpanProcessor processes & exports each span as it is created. Pros: no risk of losing a batch. Cons: app's execution is blocked until each span is processed and sent over the network
	processor := sdktrace.NewBatchSpanProcessor(exporter, sdktrace.WithBatchTimeout(cfg.BatchTimeout)) // create a batch span processor explicitly
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(cfg.Sampler),
		sdktrace.WithSpanProcessor(processor), // OR directly use: sdktrace.WithBatcher(exporter), if processor needn't be returned from the function
		sdktrace.WithResource(resources),
	)

	// Specifications for instrumentation: https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/trace/api.md
	return &Manager{traceProvider, processor, new(propagation.TraceContext)}, nil
}
