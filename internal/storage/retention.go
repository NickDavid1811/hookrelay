package storage

import (
	"context"
	"fmt"
	"hookrelay/internal/core"
	"strings"
	"time"
)

func (s *SQLite) Cleanup(ctx context.Context, now time.Time, l core.Limits) (removed int, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT e.id FROM events e WHERE EXISTS(SELECT 1 FROM deliveries d WHERE d.event_id=e.id) AND NOT EXISTS(SELECT 1 FROM deliveries d WHERE d.event_id=e.id AND NOT ((d.status='SUCCESS' AND d.terminal_at>0 AND d.terminal_at<=?) OR (d.status='DEAD' AND d.terminal_at>0 AND d.terminal_at<=?))) ORDER BY e.created,e.id LIMIT ?`, now.Add(-l.SuccessRetention).UnixMilli(), now.Add(-l.DeadRetention).UnixMilli(), l.CleanupBatch)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if len(ids) > 0 {
		placeholders := strings.Repeat("?,", len(ids))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		queries := []string{
			fmt.Sprintf("DELETE FROM attempts WHERE delivery_id IN (SELECT id FROM deliveries WHERE event_id IN (%s))", placeholders),
			fmt.Sprintf("DELETE FROM replays WHERE delivery_id IN (SELECT id FROM deliveries WHERE event_id IN (%s))", placeholders),
			fmt.Sprintf("DELETE FROM keys WHERE event_id IN (%s)", placeholders),
			fmt.Sprintf("DELETE FROM deliveries WHERE event_id IN (%s)", placeholders),
			fmt.Sprintf("DELETE FROM events WHERE id IN (%s)", placeholders),
		}
		for _, q := range queries {
			if _, err = tx.ExecContext(ctx, q, args...); err != nil {
				return 0, err
			}
		}
	}
	// Reclaim dynamic tombstones only after all retained delivery history is gone.
	// Static endpoint identities remain owned by the configuration file.
	if _, err = tx.ExecContext(ctx, `DELETE FROM endpoints WHERE id IN (SELECT ep.id FROM endpoints ep WHERE ep.active=0 AND ep.secret!='' AND NOT EXISTS(SELECT 1 FROM deliveries d WHERE d.endpoint_id=ep.id) ORDER BY ep.id LIMIT ?)`, l.CleanupBatch); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}
