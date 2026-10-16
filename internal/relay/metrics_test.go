package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

const metricsToken = "operator-token-123456789"

func scrape(s *Service, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/metrics", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func metricValue(t *testing.T, s *Service, name string, labels map[string]string) float64 {
	t.Helper()
	families, err := s.metrics().Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			match := len(metric.Label) == len(labels)
			for _, label := range metric.Label {
				if labels[label.GetName()] != label.GetValue() {
					match = false
				}
			}
			if match {
				if metric.Gauge != nil {
					return metric.Gauge.GetValue()
				}
				if metric.Counter != nil {
					return metric.Counter.GetValue()
				}
				if metric.Histogram != nil {
					return float64(metric.Histogram.GetSampleCount())
				}
			}
		}
	}
	t.Fatalf("metric not found: %s %v", name, labels)
	return 0
}
func TestMetricsAccessAndFormat(t *testing.T) {
	s, _ := setup(t)
	if scrape(s, metricsToken).Code != 404 {
		t.Fatal("metrics enabled by default")
	}
	s.MetricsToken = metricsToken
	for _, token := range []string{"", testToken, "wrong"} {
		if scrape(s, token).Code != 401 {
			t.Fatal("nonoperator accessed global metrics")
		}
	}
	w := scrape(s, metricsToken)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(w.Body.String()))
	if err != nil {
		t.Fatal("invalid Prometheus text", err)
	}
	if families["hookrelay_queue_depth"] == nil || !strings.HasSuffix(w.Body.String(), "\n") {
		t.Fatal("missing metrics")
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatal(w.Header())
	}
	for _, sensitive := range []string{testToken, testSecret, "local-client", "HOOKRELAY_SECRET", "127.0.0.1"} {
		if strings.Contains(w.Body.String(), sensitive) {
			t.Fatal("metrics leaked data", sensitive)
		}
	}
	s.DB.Close()
	if scrape(s, metricsToken).Code != 500 {
		t.Fatal("broken storage reported valid scrape")
	}
}
func TestMetricsCommitSemanticsAndRetention(t *testing.T) {
	s, _ := setup(t)
	s.MetricsToken = metricsToken
	s.Limits = DefaultLimits()
	s.Limits.ClientQueue = 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	s.testEndpoint(t, server.URL)
	w := publish(s, "key", body)
	var v Result
	json.Unmarshal(w.Body.Bytes(), &v)
	publish(s, "key", body)
	publish(s, "full", body)
	if got := metricValue(t, s, "hookrelay_publications_total", map[string]string{"result": "new"}); got != 1 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_publications_total", map[string]string{"result": "idempotent"}); got != 1 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_api_requests_total", map[string]string{"operation": "publish", "code": "429"}); got != 1 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_queue_depth", nil); got != 1 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_clients_at_queue_limit", nil); got != 1 {
		t.Fatal(got)
	}
	if err := s.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := metricValue(t, s, "hookrelay_queue_depth", nil); got != 0 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_delivery_results_total", map[string]string{"result": "SUCCESS"}); got != 1 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_delivery_http_requests_total", map[string]string{"result": "2xx"}); got != 1 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_delivery_http_duration_seconds", nil); got != 1 {
		t.Fatal(got)
	}
	if _, err := s.Cleanup(context.Background(), time.Now().Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := metricValue(t, s, "hookrelay_retention_events_deleted_total", nil); got != 1 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_events_retained", nil); got != 0 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_delivery_results_total", map[string]string{"result": "SUCCESS"}); got != 1 {
		t.Fatal("retention erased process counter", got)
	}
}
func TestMetricsFailureAndRestart(t *testing.T) {
	s, _ := setup(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	s.testEndpoint(t, server.URL)
	publish(s, "key", body)
	s.DB.Exec(`CREATE TRIGGER reject_attempt BEFORE INSERT ON attempts BEGIN SELECT RAISE(ABORT,'test'); END`)
	if err := s.step(context.Background()); err == nil {
		t.Fatal("expected persistence failure")
	}
	if got := metricValue(t, s, "hookrelay_delivery_http_requests_total", map[string]string{"result": "2xx"}); got != 1 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_delivery_results_total", map[string]string{"result": "SUCCESS"}); got != 0 {
		t.Fatal("uncommitted delivery counted", got)
	}
	fresh := &Service{DB: s.DB, Limits: s.Limits, Endpoints: s.Endpoints}
	if got := metricValue(t, fresh, "hookrelay_queue_depth", nil); got != 1 {
		t.Fatal("lost persistent queue gauge", got)
	}
	if got := metricValue(t, fresh, "hookrelay_publications_total", map[string]string{"result": "new"}); got != 0 {
		t.Fatal("process counter failed to reset", got)
	}
	s.DB.Exec(`CREATE TRIGGER reject_delete BEFORE DELETE ON events BEGIN SELECT RAISE(ABORT,'test'); END`)
	s.DB.Exec("UPDATE deliveries SET status='SUCCESS',terminal_at=1")
	if _, err := s.Cleanup(context.Background(), time.Now()); err == nil {
		t.Fatal("expected cleanup failure")
	}
	if got := metricValue(t, s, "hookrelay_cleanup_runs_total", map[string]string{"result": "error"}); got != 1 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_retention_events_deleted_total", nil); got != 0 {
		t.Fatal("failed cleanup counted", got)
	}
}
func TestMetricsConcurrentScrapesAndWrites(t *testing.T) {
	s, _ := setup(t)
	s.MetricsToken = metricsToken
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if publish(s, ID(), body).Code != 202 {
				t.Error("publish failed")
			}
			if scrape(s, metricsToken).Code != 200 {
				t.Error("scrape failed")
			}
		}()
	}
	wg.Wait()
	if got := metricValue(t, s, "hookrelay_queue_depth", nil); got != 8 {
		t.Fatal(got)
	}
}

func TestMetricsQueueAgePausedLeasesAndEligibility(t *testing.T) {
	s, _ := setup(t)
	s.MetricsToken = metricsToken
	publish(s, "key", body)
	now := time.Now()
	s.DB.Exec("UPDATE events SET created=?", now.Add(-time.Hour).UnixMilli())
	s.DB.Exec("UPDATE deliveries SET status='PROCESSING',lease=0")
	s.DB.Exec("UPDATE endpoints SET active=0")
	if got := metricValue(t, s, "hookrelay_queue_paused", nil); got != 1 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_expired_leases", nil); got != 1 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_queue_oldest_age_seconds", nil); got < 3600 || got > 3605 {
		t.Fatal(got)
	}
	s.DB.Exec("UPDATE deliveries SET status='SUCCESS',terminal_at=?", now.Add(-8*24*time.Hour).UnixMilli())
	if got := metricValue(t, s, "hookrelay_retention_events_eligible", nil); got != 1 {
		t.Fatal(got)
	}
	if got := metricValue(t, s, "hookrelay_queue_oldest_age_seconds", nil); got != 0 {
		t.Fatal("empty queue age", got)
	}
	// An operator token cannot double as a business credential.
	cfg := Config{Clients: []ClientConfig{{ID: "local-client", TokenEnv: "TOKEN", Endpoints: []EndpointConfig{{ID: "test", URL: "http://127.0.0.1:1", SecretEnv: "SECRET", AllowLoopback: true}}}}}
	if err := s.ApplyConfig(cfg, func(string) string { return metricsToken }); err == nil {
		t.Fatal("metrics credential reused as client token")
	}
}
