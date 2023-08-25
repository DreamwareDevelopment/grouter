package grouter

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

const traceProviderName = "grouter"

// newExporter returns a console exporter.
func newExporter(w io.Writer) (trace.SpanExporter, error) {
	return stdouttrace.New(
		stdouttrace.WithWriter(w),
		// Use human-readable output.
		stdouttrace.WithPrettyPrint(),
		// Do not print timestamps for the demo.
		stdouttrace.WithoutTimestamps(),
	)
}

// newResource returns a resource describing this application.
func newResource() *resource.Resource {
	// TODO: inject version
	r, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("grouter trace provider"),
			semconv.ServiceVersion("v0.1.0"),
			attribute.String("environment", "development"),
		),
	)
	return r
}

func startTracing() (func(context.Context) error, error) {
	var err error
	var id uuid.UUID

	if _, err = os.Stat("traces"); os.IsNotExist(err) {
		err = os.Mkdir("traces", 0755)
		if err != nil {
			return nil, err
		}
	}

	id, err = uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	f, err := os.Create(fmt.Sprintf("traces/%s.trace", id.String()))
	if err != nil {
		return nil, err
	}
	exp, err := newExporter(io.Writer(f))
	if err != nil {
		return nil, err
	}

	tp := trace.NewTracerProvider(trace.WithBatcher(exp), trace.WithResource(newResource()))
	otel.SetTracerProvider(tp)

	shutdown := func(ctx context.Context) error {
		var err error
		if shutdownErr := tp.Shutdown(ctx); shutdownErr != nil {
			err = shutdownErr
		}
		if closeErr := f.Close(); closeErr != nil {
			if err != nil {
				err = fmt.Errorf("%v; %v", err, closeErr)
			} else {
				err = closeErr
			}
		}
		return err
	}
	return shutdown, nil
}
