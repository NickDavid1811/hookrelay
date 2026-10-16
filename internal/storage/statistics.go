package storage

import (
	"context"
	"database/sql"
	"hookrelay/internal/core"
	"time"
)

var _ core.StatisticsSource = (*SQLite)(nil)

func (s *SQLite) Snapshot(ctx context.Context, now time.Time, l core.Limits) (core.StorageStats, error) {
	tx, err := s.reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return core.StorageStats{}, err
	}
	defer tx.Rollback()
	states := map[string]float64{}
	values := map[string]float64{}
	rows, err := tx.QueryContext(ctx, `SELECT CASE WHEN status IN ('PENDING','RETRYING','PROCESSING','SUCCESS','DEAD') THEN status ELSE 'UNKNOWN' END,count(*) FROM deliveries GROUP BY 1`)
	if err != nil {
		return core.StorageStats{}, err
	}
	for rows.Next() {
		var state string
		var count float64
		if err = rows.Scan(&state, &count); err != nil {
			rows.Close()
			return core.StorageStats{}, err
		}
		states[state] = count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return core.StorageStats{}, err
	}
	values["queue_depth"] = states["PENDING"] + states["RETRYING"] + states["PROCESSING"]
	var oldest sql.NullInt64
	var paused float64
	err = tx.QueryRowContext(ctx, `SELECT min(e.created),coalesce(sum(CASE WHEN ep.active=0 OR c.active=0 THEN 1 ELSE 0 END),0) FROM deliveries d JOIN events e ON e.id=d.event_id JOIN endpoints ep ON ep.id=d.endpoint_id JOIN clients c ON c.id=e.client_id WHERE d.status IN ('PENDING','RETRYING','PROCESSING')`).Scan(&oldest, &paused)
	if err != nil {
		return core.StorageStats{}, err
	}
	age := float64(0)
	if oldest.Valid {
		age = now.Sub(time.UnixMilli(oldest.Int64)).Seconds()
		if age < 0 {
			age = 0
		}
	}
	values["queue_oldest_age_seconds"] = age
	values["queue_paused"] = paused
	var expired, saturated, events, bytes, eligible float64
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM deliveries WHERE status='PROCESSING' AND lease<=?", now.UnixMilli()).Scan(&expired); err != nil {
		return core.StorageStats{}, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT e.client_id FROM deliveries d JOIN events e ON e.id=d.event_id WHERE d.status IN ('PENDING','RETRYING','PROCESSING') GROUP BY e.client_id HAVING count(*)>=?)`, l.ClientQueue).Scan(&saturated); err != nil {
		return core.StorageStats{}, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(length(body)),0) FROM events").Scan(&events, &bytes); err != nil {
		return core.StorageStats{}, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM events e WHERE EXISTS(SELECT 1 FROM deliveries d WHERE d.event_id=e.id) AND NOT EXISTS(SELECT 1 FROM deliveries d WHERE d.event_id=e.id AND NOT ((d.status='SUCCESS' AND d.terminal_at>0 AND d.terminal_at<=?) OR (d.status='DEAD' AND d.terminal_at>0 AND d.terminal_at<=?)))`, now.Add(-l.SuccessRetention).UnixMilli(), now.Add(-l.DeadRetention).UnixMilli()).Scan(&eligible); err != nil {
		return core.StorageStats{}, err
	}
	if err = tx.Commit(); err != nil {
		return core.StorageStats{}, err
	}
	values["expired_leases"] = expired
	values["clients_at_queue_limit"] = saturated
	values["events_retained"] = events
	values["payload_bytes_retained"] = bytes
	values["retention_events_eligible"] = eligible
	values["queue_limit"] = float64(l.Queue)
	values["client_queue_limit"] = float64(l.ClientQueue)
	values["events_limit"] = float64(l.Events)
	values["payload_bytes_limit"] = float64(l.PayloadBytes)
	values["cycles_limit"] = float64(l.Cycles)
	values["success_retention_seconds"] = l.SuccessRetention.Seconds()
	values["dead_retention_seconds"] = l.DeadRetention.Seconds()
	values["cleanup_interval_seconds"] = l.CleanupInterval.Seconds()
	values["cleanup_batch_limit"] = float64(l.CleanupBatch)
	return core.StorageStats{States: states, Values: values}, nil
}
