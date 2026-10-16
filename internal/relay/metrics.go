package relay

import (
	"context"
	"hookrelay/internal/core"
	"hookrelay/internal/observability"
	"hookrelay/internal/storage"
	"net/http"
	"time"
)

func (s *Service) metrics() *observability.Metrics {
	s.metricsOnce.Do(func() {
		s.observability = observability.New(func(ctx context.Context) (core.StorageStats, error) {
			return (&storage.SQLite{DB: s.DB, ReadDB: s.ReadDB}).Snapshot(ctx, time.Now(), s.limits())
		})
	})
	return s.observability
}
func (s *Service) scrape(w http.ResponseWriter, r *http.Request) {
	s.metrics().Scrape(s.MetricsToken, w, r)
}
