// Package observability implements Prometheus telemetry instrumentation and metric collectors.
package observability

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"hookrelay/internal/core"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics manages the Prometheus registry and metric collectors for operations, deliveries, and queue states.
type Metrics struct {
	Registry        *prometheus.Registry
	requests        *prometheus.CounterVec
	publications    *prometheus.CounterVec
	deliveryHTTP    *prometheus.CounterVec
	deliveryResults *prometheus.CounterVec
	httpDuration    prometheus.Histogram
	cleanup         *prometheus.CounterVec
	cleaned         prometheus.Counter
	lastCleanup     prometheus.Gauge
	worker          prometheus.Gauge
}

// New initializes Prometheus metrics, creates a private registry, and registers collectors for storage snapshots.
func New(snapshot func(context.Context) (core.StorageStats, error)) *Metrics {
	m := &Metrics{Registry: prometheus.NewRegistry(),
		requests:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "hookrelay_api_requests_total", Help: "Business POST responses in this process."}, []string{"operation", "code"}),
		publications:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "hookrelay_publications_total", Help: "Committed new events and accepted idempotent receipts in this process."}, []string{"result"}),
		deliveryHTTP:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "hookrelay_delivery_http_requests_total", Help: "Outbound HTTP observations, including sends not finalized in SQLite."}, []string{"result"}),
		deliveryResults: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "hookrelay_delivery_results_total", Help: "Delivery results committed in SQLite by this process."}, []string{"result"}),
		httpDuration:    prometheus.NewHistogram(prometheus.HistogramOpts{Name: "hookrelay_delivery_http_duration_seconds", Help: "Outbound request duration through bounded response read/close, excluding SQLite finalization.", Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10}}),
		cleanup:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "hookrelay_cleanup_runs_total", Help: "Retention batches completed or failed by this process."}, []string{"result"}),
		cleaned:         prometheus.NewCounter(prometheus.CounterOpts{Name: "hookrelay_retention_events_deleted_total", Help: "Event graphs deleted by successful retention transactions in this process."}),
		lastCleanup:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "hookrelay_cleanup_last_success_timestamp_seconds", Help: "Unix time of last successful retention batch, zero until first batch."}),
		worker:          prometheus.NewGauge(prometheus.GaugeOpts{Name: "hookrelay_worker_running", Help: "Whether the worker Run loop is active."}),
	}
	for _, value := range []string{"new", "idempotent"} {
		m.publications.WithLabelValues(value)
	}
	for _, value := range []string{"2xx", "3xx", "4xx", "5xx", "other", "transport_error", "response_read_error"} {
		m.deliveryHTTP.WithLabelValues(value)
	}
	for _, value := range []string{"SUCCESS", "RETRYING", "DEAD"} {
		m.deliveryResults.WithLabelValues(value)
	}
	for _, value := range []string{"success", "error"} {
		m.cleanup.WithLabelValues(value)
	}
	// Fixed response codes avoid cardinality growth from arbitrary routes/statuses.
	for _, operation := range []string{"publish", "replay"} {
		for _, code := range []string{"202", "400", "401", "404", "405", "409", "413", "429", "503", "other"} {
			m.requests.WithLabelValues(operation, code)
		}
	}
	m.Registry.MustRegister(m.requests, m.publications, m.deliveryHTTP, m.deliveryResults, m.httpDuration, m.cleanup, m.cleaned, m.lastCleanup, m.worker, &storageCollector{snapshotSource: snapshot})
	return m
}

// Scrape handles the Prometheus scrape request, enforcing bearer token authentication against constant-time comparison.
func (m *Metrics) Scrape(token string, w http.ResponseWriter, r *http.Request) {
	if token == "" {
		fail(w, 404, "metrics disabled")
		return
	}
	if !hmac.Equal([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) {
		fail(w, 401, "unauthorized")
		return
	}
	// Registry is private to the service; no high-cardinality default instrumentation.
	promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{ErrorHandling: promhttp.HTTPErrorOnError}).ServeHTTP(w, r)
}
func responseCode(code int) string {
	switch code {
	case 202:
		return "202"
	case 400:
		return "400"
	case 401:
		return "401"
	case 404:
		return "404"
	case 405:
		return "405"
	case 409:
		return "409"
	case 413:
		return "413"
	case 429:
		return "429"
	case 503:
		return "503"
	default:
		return "other"
	}
}
func httpResult(code int, reason string) string {
	if reason == "transport failure" {
		return "transport_error"
	}
	if reason != "" {
		return "response_read_error"
	}
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}

type storageCollector struct {
	snapshotSource func(context.Context) (core.StorageStats, error)
}

var stateDesc = prometheus.NewDesc("hookrelay_deliveries", "Retained deliveries by state, including paused clients/endpoints.", []string{"state"}, nil)
var gaugeHelp = map[string]string{
	"hookrelay_queue_depth":               "All unfinished deliveries, including paused work.",
	"hookrelay_queue_oldest_age_seconds":  "Age since publication of oldest unfinished delivery, zero if empty.",
	"hookrelay_queue_paused":              "Unfinished deliveries with an inactive endpoint or client.",
	"hookrelay_expired_leases":            "PROCESSING deliveries whose lease has expired, including paused work.",
	"hookrelay_clients_at_queue_limit":    "Clients whose unfinished count meets or exceeds the configured client cap.",
	"hookrelay_events_retained":           "Number of retained events.",
	"hookrelay_payload_bytes_retained":    "Bytes of retained event payloads, not physical disk size.",
	"hookrelay_retention_events_eligible": "Complete event graphs currently old enough to delete.",
	"hookrelay_queue_limit":               "Global unfinished delivery cap.",
	"hookrelay_client_queue_limit":        "Configured unfinished delivery cap per client.",
	"hookrelay_events_limit":              "Retained event cap.",
	"hookrelay_payload_bytes_limit":       "Retained payload byte cap.",
	"hookrelay_cycles_limit":              "Configured maximum cycles per delivery.",
	"hookrelay_success_retention_seconds": "Configured SUCCESS retention.",
	"hookrelay_dead_retention_seconds":    "Configured DEAD retention.",
	"hookrelay_cleanup_interval_seconds":  "Configured interval between cleanup batches.",
	"hookrelay_cleanup_batch_limit":       "Maximum event graphs per retention batch.",
}

func gaugeDesc(name string) *prometheus.Desc {
	return prometheus.NewDesc(name, gaugeHelp[name], nil, nil)
}
func (c *storageCollector) Describe(out chan<- *prometheus.Desc) {
	out <- stateDesc
	for name := range gaugeHelp {
		out <- gaugeDesc(name)
	}
}
func (c *storageCollector) Collect(out chan<- prometheus.Metric) {
	states, values, err := c.snapshot()
	if err != nil {
		out <- prometheus.NewInvalidMetric(stateDesc, errors.New("storage snapshot unavailable"))
		return
	}
	for _, state := range []string{"PENDING", "RETRYING", "PROCESSING", "SUCCESS", "DEAD", "UNKNOWN"} {
		out <- prometheus.MustNewConstMetric(stateDesc, prometheus.GaugeValue, states[state], state)
	}
	for name, value := range values {
		out <- prometheus.MustNewConstMetric(gaugeDesc(name), prometheus.GaugeValue, value)
	}
}
func (c *storageCollector) snapshot() (map[string]float64, map[string]float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stats, err := c.snapshotSource(ctx)
	values := make(map[string]float64, len(stats.Values))
	for name, value := range stats.Values {
		values["hookrelay_"+name] = value
	}
	return stats.States, values, err
}

func fail(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

var _ core.Observer = (*Metrics)(nil)

func (m *Metrics) Response(operation string, code int) {
	m.requests.WithLabelValues(operation, responseCode(code)).Inc()
}
func (m *Metrics) Publication(value string) { m.publications.WithLabelValues(value).Inc() }
func (m *Metrics) HTTP(value core.HTTPResult) {
	m.httpDuration.Observe(value.Duration.Seconds())
	m.deliveryHTTP.WithLabelValues(httpResult(value.Code, value.Error)).Inc()
}
func (m *Metrics) Delivery(status string) { m.deliveryResults.WithLabelValues(status).Inc() }
func (m *Metrics) Cleanup(err error, n int, now time.Time) {
	if err != nil {
		m.cleanup.WithLabelValues("error").Inc()
		return
	}
	m.cleanup.WithLabelValues("success").Inc()
	m.cleaned.Add(float64(n))
	m.lastCleanup.Set(float64(now.UnixMilli()) / 1000)
}
func (m *Metrics) Worker(running bool) {
	v := float64(0)
	if running {
		v = 1
	}
	m.worker.Set(v)
}
