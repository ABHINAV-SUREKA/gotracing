package main

import (
	"context"

	"github.com/ABHINAV-SUREKA/gotracing/tracing"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	manager, err := tracing.New(context.Background(), tracing.Config{
		Endpoint: "localhost:4317",
		Sampler:  sdktrace.AlwaysSample(),
		// DebugOutput: os.Stderr,
		Attributes: map[string]string{
			"service.name":      "test-service",
			"service.namespace": "test-service-namespace",
			"library.language":  "go",
		},
	})

	if err != nil {
		log.Errorf("Could not create Tracer Provider: %s", err)
	}

	otel.SetTracerProvider(manager.TracerProvider)

	/* Traces can extend beyond a single process.
	This requires context propagation of identifiers for a trace to remote processes over the wire.
	*/
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(manager.Propagator, propagation.Baggage{}))
}
