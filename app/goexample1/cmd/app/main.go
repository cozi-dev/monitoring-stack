package main

import (
	"context"
	"fmt"
	"goexample/pkg/kafkapkg"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	logger *logrus.Logger
)

// logWithTrace returns a logrus.Entry with trace_id and span_id from context
func logWithTrace(ctx context.Context) *logrus.Entry {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		return logger.WithFields(logrus.Fields{
			"trace_id": span.SpanContext().TraceID().String(),
			"span_id":  span.SpanContext().SpanID().String(),
		})
	}
	return logger.WithFields(logrus.Fields{})
}

func hello(w http.ResponseWriter, req *http.Request) {
	// Extract the context from the incoming HTTP headers
	parentCtx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.HeaderCarrier(req.Header))
	_, span := tracer.Start(parentCtx, "Start hello handler")
	defer span.End()

	logWithTrace(parentCtx).WithFields(logrus.Fields{
		"method": req.Method,
		"path":   req.URL.Path,
	}).Info("Handling hello request")

	span.AddEvent("hello again from goexample1", trace.WithAttributes(attribute.Int("test", 1)))
	span.SetAttributes(attribute.String("hello", "world"))
	fmt.Fprintf(w, "hello again\n")

	// sent to rustexample:8080
	appreq, _ := http.NewRequest("GET", "http://rustexample:8080", nil)
	otel.GetTextMapPropagator().Inject(parentCtx, propagation.HeaderCarrier(appreq.Header))
	res, err := http.DefaultClient.Do(appreq)
	if err != nil {
		logWithTrace(parentCtx).WithFields(logrus.Fields{
			"error":   err,
			"service": "rustexample",
		}).Error("Failed to send request")
	}
	bodyB, _ := io.ReadAll(res.Body)
	span.SetAttributes(attribute.String("response", string(bodyB)))
}

func headers(w http.ResponseWriter, req *http.Request) {
	for name, headers := range req.Header {
		for _, h := range headers {
			fmt.Fprintf(w, "%v: %v\n", name, h)
		}
	}
}

func main() {
	ctx := context.Background()

	// Initialize Logrus logger
	logger = logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)

	logger.WithFields(logrus.Fields{
		"service": "goexample1",
		"port":    "8080",
	}).Info("Starting goexample1 service")

	// For testing to print out traces to the console
	// exp, err := newConsoleExporter()
	exp, err := newOTLPExporter(ctx)

	if err != nil {
		logger.WithField("error", err).Fatal("failed to initialize exporter")
	}

	// Create a new tracer provider with a batch span processor and the given exporter.
	tp := newTraceProvider(exp)

	// Handle shutdown properly so nothing leaks.
	defer func() { _ = tp.Shutdown(ctx) }()

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Finally, set the tracer that can be used for this package.
	tracer = tp.Tracer("goexample1")

	// kafka
	go kakaConsumer()

	// routes
	http.HandleFunc("/hello", hello)
	http.HandleFunc("/headers", headers)

	logger.Info("Server is ready to handle requests")
	http.ListenAndServe(":8080", nil)
}

func kakaConsumer() {
	reader := kafkapkg.GetKafkaReader("trace", "go")
	defer reader.Close()

	logger.Info("start consuming kafka messages")
	for {
		m, err := reader.ReadMessage(context.Background())
		if err != nil {
			logger.WithField("error", err).Fatal("Error reading kafka message")
		}

		// Extract the context from Kafka headers
		carrier := propagation.MapCarrier{}
		for _, header := range m.Headers {
			carrier[header.Key] = string(header.Value)
		}

		// Extract the tracing context from the carrier
		ctx := otel.GetTextMapPropagator().Extract(context.Background(), carrier)

		// Start a new span with the extracted context
		_, span := tracer.Start(ctx, "Processing kafka message")
		span.SetAttributes(attribute.String("message", string(m.Value)))

		logWithTrace(ctx).WithFields(logrus.Fields{
			"topic":     m.Topic,
			"partition": m.Partition,
			"offset":    m.Offset,
			"key":       string(m.Key),
			"value":     string(m.Value),
		}).Info("Received kafka message")

		span.End()
	}
}

var (
	tracer       trace.Tracer
	otlpEndpoint string
)

func init() {
	otlpEndpoint = os.Getenv("OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		log.Fatalln("You MUST set OTLP_ENDPOINT env variable!")
	}
}

// List of supported exporters
// https://opentelemetry.io/docs/instrumentation/go/exporters/

// Console Exporter, only for testing
// func newConsoleExporter() (sdktrace.SpanExporter, error) {
// 	return stdouttrace.New()
// }

// OTLP Exporter
func newOTLPExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	// Change default HTTPS -> HTTP
	insecureOpt := otlptracehttp.WithInsecure()

	// Update default OTLP reciver endpoint
	endpointOpt := otlptracehttp.WithEndpoint(otlpEndpoint)

	return otlptracehttp.New(ctx, insecureOpt, endpointOpt)
}

// TracerProvider is an OpenTelemetry TracerProvider.
// It provides Tracers to instrumentation so it can trace operational flow through a system.
func newTraceProvider(exp sdktrace.SpanExporter) *sdktrace.TracerProvider {
	// Ensure default SDK resources and the required service name are set.
	r, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("goexample1"),
		),
	)

	if err != nil {
		panic(err)
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(r),
	)
}
