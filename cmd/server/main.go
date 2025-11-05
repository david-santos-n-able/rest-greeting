package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.26.0"
)

type greetingResponse struct {
	Message string `json:"message"`
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

const (
	defaultHTTPAddr    = ":8080"
	defaultMetricsAddr = ":9092"
)

func initTracer(ctx context.Context) (*sdktrace.TracerProvider, error) {
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(
		ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String("rest-greeting"),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return tp, nil
}

func main() {
	httpAddr := flag.String("http-addr", defaultHTTPAddr, "HTTP listen address")
	metricsAddr := flag.String("metrics-addr", defaultMetricsAddr, "Prometheus metrics listen address")
	flag.Parse()

	tp, err := initTracer(context.Background())
	if err != nil {
		log.Fatalf("failed to set up tracing: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			log.Printf("tracer provider shutdown failed: %v", err)
		}
	}()

	requestCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests processed.",
		},
		[]string{"method", "path", "status"},
	)

	requestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Histogram of latencies for HTTP requests.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(requestCounter)
	registry.MustRegister(requestDuration)
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	registry.MustRegister(collectors.NewGoCollector())

	mux := http.NewServeMux()
	mux.Handle("/hello", instrumentHandler("/hello", requestCounter, requestDuration, http.HandlerFunc(helloHandler)))

	httpServer := &http.Server{
		Addr:    *httpAddr,
		Handler: mux,
	}

	metricsServer := &http.Server{
		Addr:    *metricsAddr,
		Handler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
	}

	go func() {
		log.Printf("HTTP server listening on %s", *httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	go func() {
		log.Printf("Prometheus metrics listening on %s", *metricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("metrics server failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("received termination signal, shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = httpServer.Shutdown(shutdownCtx)
	_ = metricsServer.Shutdown(shutdownCtx)

	log.Println("shutdown complete")
}

func instrumentHandler(path string, counter *prometheus.CounterVec, duration *prometheus.HistogramVec, handler http.Handler) http.Handler {
	otelHandler := otelhttp.NewHandler(handler, path)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()

		otelHandler.ServeHTTP(recorder, r)

		elapsed := time.Since(start).Seconds()
		statusCode := recorder.status
		labels := prometheus.Labels{
			"method": r.Method,
			"path":   path,
			"status": strconv.Itoa(statusCode),
		}
		counter.With(labels).Inc()
		duration.With(labels).Observe(elapsed)
	})
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		name = "World"
	}

	w.Header().Set("Content-Type", "application/json")
	resp := greetingResponse{Message: "Hello " + name}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
