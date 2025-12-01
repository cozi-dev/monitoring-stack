package main

import (
	"context"
	"errors"
	"fmt"
	"goexample/pkg/kafkapkg"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	kafka "github.com/segmentio/kafka-go"
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
	kafkaWriter *kafka.Writer
	logger      *logrus.Logger
	rng         *rand.Rand

	// Prometheus metrics
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets, // Default buckets: 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
		},
		[]string{"method", "endpoint", "status"},
	)
)

func init() {
	// Register Prometheus metrics
	prometheus.MustRegister(httpRequestsTotal)
	prometheus.MustRegister(httpRequestDuration)
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{w, http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// metricsMiddleware wraps an HTTP handler with Prometheus metrics
func metricsMiddleware(endpoint string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := newResponseWriter(w)

		// Call the actual handler
		handler(rw, r)

		duration := time.Since(start).Seconds()
		statusCode := strconv.Itoa(rw.statusCode)

		// Record metrics
		httpRequestsTotal.WithLabelValues(r.Method, endpoint, statusCode).Inc()
		httpRequestDuration.WithLabelValues(r.Method, endpoint, statusCode).Observe(duration)
	}
}

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
	ctx, span := tracer.Start(req.Context(), "Start hello handler")
	defer span.End()

	logWithTrace(ctx).WithFields(logrus.Fields{
		"method": req.Method,
		"path":   req.URL.Path,
	}).Info("Handling hello request")

	// Randomly return 500 error (30% chance)
	if rng.Float32() < 0.3 {
		span.RecordError(errors.New("random internal server error"))
		logWithTrace(ctx).WithFields(logrus.Fields{
			"method": req.Method,
			"path":   req.URL.Path,
		}).Error("Random internal server error")

		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Internal Server Error\n")
		return
	}

	// send http request to goexample1:8080
	appreq, _ := http.NewRequest("GET", "http://goexample1:8080/hello", nil)
	// Use the propagators from the global Propagation to inject the current context into req.
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(appreq.Header))

	res, err := http.DefaultClient.Do(appreq)
	if err != nil {
		logWithTrace(ctx).WithFields(logrus.Fields{
			"error":   err,
			"service": "goexample1",
		}).Error("Failed to send request")
	}

	// print response body ouput
	bodyB, _ := io.ReadAll(res.Body)
	span.SetAttributes(attribute.String("response", string(bodyB)))

	subHello(ctx)
	sendHelloKafkaMsg(ctx)

	fmt.Fprintf(w, "hello\n")
}

func sendHelloKafkaMsg(ctx context.Context) (err error) {
	_, span := tracer.Start(ctx, "Sending hello message to kafka")
	defer span.End()

	// Create a map carrier to hold the propagated context
	carrier := propagation.MapCarrier{}

	// Inject the tracing context into the carrier
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	// Convert the carrier to Kafka headers
	headers := make([]kafka.Header, 0, len(carrier))
	for key, value := range carrier {
		headers = append(headers, kafka.Header{
			Key:   key,
			Value: []byte(value),
		})
	}

	msg := kafka.Message{
		Key:     []byte("test-message-goexample"),
		Value:   []byte("hello from goexample"),
		Headers: headers,
	}
	err = kafkaWriter.WriteMessages(ctx, msg)
	if err != nil {
		logWithTrace(ctx).WithFields(logrus.Fields{
			"error":       err,
			"topic":       "trace",
			"message_key": "test-message-goexample",
		}).Error("Error sending message to kafka")
	}
	return
}

func subHello(ctx context.Context) {
	_, span := tracer.Start(ctx, "Start subHello handler")
	defer span.End()

	// Simulate long processing time
	time.Sleep(100 * time.Millisecond)
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
	rng = rand.New(rand.NewSource(time.Now().UnixNano()))

	// Initialize Logrus logger
	logger = logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetLevel(logrus.InfoLevel)

	logger.WithFields(logrus.Fields{
		"service": "goexample",
		"port":    "8080",
	}).Info("Starting goexample service")

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
	tracer = tp.Tracer("goexample")

	// Kafka writer
	kafkaWriter = kafkapkg.GetKafkaWriter("trace")

	// routes
	http.HandleFunc("/hello", metricsMiddleware("/hello", hello))
	http.HandleFunc("/headers", metricsMiddleware("/headers", headers))

	// Prometheus metrics endpoint
	http.Handle("/metrics", promhttp.Handler())

	logger.Info("Server is ready to handle requests")
	http.ListenAndServe(":8080", nil)
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
			semconv.ServiceName("goexample"),
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
