// Package storage implements atomic persistence ports with SQLite.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"hookrelay/internal/core"
	"time"
)

// DefaultClaimLeaseDurationMs defines the default 30-second atomic lease duration for claimed deliveries.
const DefaultClaimLeaseDurationMs int64 = 30000

// ErrLeaseLost indicates that a worker lease expired or was preempted before finalization.
var ErrLeaseLost = errors.New("lease lost")

// SQLite implements the persistence ports defined in core.Store, using pure-Go SQLite.
// It supports an optional ReadDB pool for concurrent read-only queries alongside the serialized DB writer.
type SQLite struct {
	DB     *sql.DB
	ReadDB *sql.DB
}

func (s *SQLite) reader() *sql.DB {
	if s.ReadDB != nil {
		return s.ReadDB
	}
	return s.DB
}

var _ core.Store = (*SQLite)(nil)

func (s *SQLite) Ping(ctx context.Context) error { return s.reader().PingContext(ctx) }
func (s *SQLite) Authenticate(ctx context.Context, hash string) (string, error) {
	var id string
	err := s.reader().QueryRowContext(ctx, "SELECT id FROM clients WHERE active=1 AND token_hash=?", hash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", core.Failure(core.Unauthorized, "unauthorized")
	}
	return id, err
}
func (s *SQLite) Owns(ctx context.Context, client, id string) error {
	var n int
	err := s.reader().QueryRowContext(ctx, "SELECT 1 FROM deliveries d JOIN events e ON e.id=d.event_id WHERE d.id=? AND e.client_id=?", id, client).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Failure(core.NotFound, "delivery not found")
	}
	return err
}
func admit(ctx context.Context, tx *sql.Tx, client string, payload int, newEvent bool, l core.Limits) error {
	if int64(payload) > l.PayloadBytes {
		return core.Failure(core.Capacity, "storage or global queue capacity reached")
	}
	var global, tenant int
	err := tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(CASE WHEN e.client_id=? THEN 1 ELSE 0 END),0) FROM deliveries d JOIN events e ON e.id=d.event_id WHERE d.status IN ('PENDING','RETRYING','PROCESSING')`, client).Scan(&global, &tenant)
	if err != nil {
		return err
	}
	if tenant >= l.ClientQueue {
		return core.Failure(core.ClientCapacity, "client queue capacity reached")
	}
	if global >= l.Queue {
		return core.Failure(core.Capacity, "storage or global queue capacity reached")
	}
	var events int
	var bytes int64
	if newEvent {
		if err = tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(length(body)),0) FROM events").Scan(&events, &bytes); err != nil {
			return err
		}
	}
	return core.CapacityError(core.CapacityStats{
		GlobalQueue:  global,
		TenantQueue:  tenant,
		TotalEvents:  events,
		PayloadBytes: bytes,
		NewPayload:   payload,
		IsNewEvent:   newEvent,
	}, l)
}
func (s *SQLite) Publish(ctx context.Context, c core.PublishCommand, l core.Limits) (core.Result, bool, error) {
	var result core.Result
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, false, err
	}
	defer tx.Rollback()

	var old string
	err = tx.QueryRowContext(ctx, "SELECT hash,event_id,delivery_id FROM keys WHERE client_id=? AND key=?", c.Client, c.Key).Scan(&old, &result.EventID, &result.DeliveryID)
	if err == nil {
		if old != c.Hash {
			return result, false, core.Failure(core.Conflict, "key reused with different bytes")
		}
		result.Status = "PENDING"
		result.Cycle = 1
		return result, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, false, err
	}

	var authorized int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM endpoints WHERE id=? AND client_id=? AND active=1", c.Publication.EndpointID, c.Client).Scan(&authorized)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, core.Failure(core.NotFound, "endpoint not found")
	}
	if err != nil {
		return result, false, err
	}

	if err = admit(ctx, tx, c.Client, len(c.Publication.Payload), true, l); err != nil {
		return result, false, err
	}

	result = core.Result{
		EventID:    c.EventID,
		DeliveryID: c.DeliveryID,
		Status:     "PENDING",
		Cycle:      1,
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO events(id,kind,body,created,client_id) VALUES(?,?,?,?,?)", c.EventID, c.Publication.EventType, []byte(c.Publication.Payload), c.Created, c.Client)
	if err != nil {
		return result, false, err
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO deliveries(id,event_id,status,endpoint_id) VALUES(?,?,?,?)", c.DeliveryID, c.EventID, "PENDING", c.Publication.EndpointID)
	if err != nil {
		return result, false, err
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO keys VALUES(?,?,?,?,?)", c.Client, c.Key, c.Hash, c.EventID, c.DeliveryID)
	if err != nil {
		return result, false, err
	}

	if err = tx.Commit(); err != nil {
		return result, false, err
	}

	return result, false, nil
}
func (s *SQLite) Delivery(ctx context.Context, client, id string) (core.Result, error) {
	var v core.Result
	err := s.reader().QueryRowContext(ctx, "SELECT event_id,d.id,status,attempts,http_status,error,cycle,cycle_attempts FROM deliveries d JOIN events e ON e.id=d.event_id WHERE d.id=? AND e.client_id=?", id, client).Scan(&v.EventID, &v.DeliveryID, &v.Status, &v.Attempts, &v.HTTPStatus, &v.Error, &v.Cycle, &v.CycleAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		err = core.Failure(core.NotFound, "delivery not found")
	}
	return v, err
}
func (s *SQLite) Attempts(ctx context.Context, client, id string, after int) ([]core.AttemptRecord, error) {
	if err := s.Owns(ctx, client, id); err != nil {
		return nil, err
	}
	rows, err := s.reader().QueryContext(ctx, "SELECT number,http_status,error,completed,cycle FROM attempts WHERE delivery_id=? AND number>? ORDER BY number LIMIT 100", id, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []core.AttemptRecord{}
	for rows.Next() {
		var a core.AttemptRecord
		if err = rows.Scan(&a.Number, &a.HTTPStatus, &a.Error, &a.CompletedUnixMs, &a.Cycle); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLite) Replays(ctx context.Context, client, id string, after int) ([]core.ReplayRecord, error) {
	if err := s.Owns(ctx, client, id); err != nil {
		return nil, err
	}
	rows, err := s.reader().QueryContext(ctx, "SELECT cycle,actor,reason,requested FROM replays WHERE delivery_id=? AND cycle>? ORDER BY cycle LIMIT 100", id, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []core.ReplayRecord{}
	for rows.Next() {
		var r core.ReplayRecord
		if err = rows.Scan(&r.Cycle, &r.Actor, &r.Reason, &r.RequestedUnixMs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLite) Endpoints(ctx context.Context, client, after string) ([]core.EndpointRecord, error) {
	rows, err := s.reader().QueryContext(ctx, "SELECT id,url FROM endpoints WHERE client_id=? AND active=1 AND id>? ORDER BY id LIMIT 1000", client, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []core.EndpointRecord{}
	for rows.Next() {
		var ep core.EndpointRecord
		if err = rows.Scan(&ep.ID, &ep.URL); err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

func (s *SQLite) Replay(ctx context.Context, c core.ReplayCommand, l core.Limits) (core.Result, error) {
	var out core.Result
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	var status string
	var cycle int
	err = tx.QueryRowContext(ctx, "SELECT status,cycle FROM deliveries d JOIN events e ON e.id=d.event_id WHERE d.id=? AND e.client_id=?", c.ID, c.Client).Scan(&status, &cycle)
	if errors.Is(err, sql.ErrNoRows) {
		return out, core.Failure(core.NotFound, "delivery not found")
	}
	if err != nil {
		return out, err
	}

	if err = core.ReplayAllowed(status, cycle, c.ExpectedCycle, l.Cycles); err != nil {
		return out, err
	}
	if err = admit(ctx, tx, c.Client, 0, false, l); err != nil {
		return out, err
	}

	err = tx.QueryRowContext(ctx, `UPDATE deliveries SET status='PENDING',cycle=cycle+1,cycle_attempts=0,next=0,lease=0,token='',http_status=0,error='',terminal_at=0 WHERE id=? AND status='DEAD' AND cycle=? AND endpoint_id IN (SELECT id FROM endpoints WHERE active=1) RETURNING event_id,id,status,attempts,cycle,cycle_attempts`, c.ID, c.ExpectedCycle).Scan(&out.EventID, &out.DeliveryID, &out.Status, &out.Attempts, &out.Cycle, &out.CycleAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return out, core.Failure(core.Conflict, "replay requires DEAD and matching expected_cycle")
	}
	if err != nil {
		return out, err
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO replays VALUES(?,?,?,?,?)", out.DeliveryID, out.Cycle, c.Client, c.Reason, c.Requested)
	if err != nil {
		return out, err
	}

	if err = tx.Commit(); err != nil {
		return out, err
	}

	return out, nil
}

func (s *SQLite) Claim(ctx context.Context, now int64, token string) (core.Claim, bool, error) {
	c := core.Claim{Token: token}
	err := s.DB.QueryRowContext(ctx, `UPDATE deliveries SET status='PROCESSING',token=?,lease=? WHERE id=(SELECT d.id FROM deliveries d JOIN endpoints ep ON ep.id=d.endpoint_id JOIN clients c ON c.id=ep.client_id WHERE ep.active=1 AND c.active=1 AND ((d.status IN ('PENDING','RETRYING') AND next<=?) OR (d.status='PROCESSING' AND lease<=?)) ORDER BY d.rowid LIMIT 1) RETURNING id,event_id,attempts,cycle,cycle_attempts,endpoint_id`, token, now+DefaultClaimLeaseDurationMs, now, now).Scan(&c.ID, &c.EventID, &c.Attempts, &c.Cycle, &c.CycleAttempts, &c.EndpointID)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}
func (s *SQLite) Payload(ctx context.Context, event string) ([]byte, error) {
	var body []byte
	err := s.reader().QueryRowContext(ctx, "SELECT body FROM events WHERE id=?", event).Scan(&body)
	return body, err
}
func (s *SQLite) Finalize(ctx context.Context, c core.Completion) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "UPDATE deliveries SET status=?,attempts=attempts+1,cycle_attempts=cycle_attempts+1,next=?,lease=0,token='',http_status=?,error=?,terminal_at=? WHERE id=? AND token=? AND lease>?", c.Status, c.Next, c.Code, c.Error, c.Terminal, c.Claim.ID, c.Claim.Token, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLeaseLost
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO attempts(delivery_id,number,http_status,error,completed,cycle) VALUES(?,?,?,?,?,?)", c.Claim.ID, c.Claim.Attempts+1, c.Code, c.Error, c.Completed, c.Claim.Cycle); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) CreateEndpoint(ctx context.Context, c core.CreateEndpointCommand, l core.Limits) (core.EndpointCreated, error) {
	out := core.EndpointCreated{ID: c.ID, URL: c.URL, Secret: c.Secret}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var existing int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM endpoints WHERE id=?", c.ID).Scan(&existing)
	if err == nil {
		return out, core.Failure(core.Conflict, "endpoint ID already exists")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	var global, tenant int
	err = tx.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(CASE WHEN client_id=? THEN 1 ELSE 0 END),0) FROM endpoints", c.ClientID).Scan(&global, &tenant)
	if err != nil {
		return out, err
	}
	if tenant >= l.ClientEndpoints {
		return out, core.Failure(core.ClientCapacity, "client endpoint capacity reached")
	}
	if global >= l.Endpoints {
		return out, core.Failure(core.Capacity, "global endpoint capacity reached")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO endpoints(id,client_id,url,secret_env,allow_loopback,active,secret) VALUES(?,?,?,?,?,1,?)", c.ID, c.ClientID, c.URL, "", c.AllowLoopback, c.Secret)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *SQLite) GetEndpoint(ctx context.Context, client, id string) (core.EndpointRecord, error) {
	var ep core.EndpointRecord
	err := s.reader().QueryRowContext(ctx, "SELECT id, url FROM endpoints WHERE id=? AND client_id=? AND active=1", id, client).Scan(&ep.ID, &ep.URL)
	if errors.Is(err, sql.ErrNoRows) {
		return ep, core.Failure(core.NotFound, "endpoint not found")
	}
	return ep, err
}

// DeleteEndpoint and Claim share the serialized writer. A claim/publication cannot slip
// between the unfinished-work check and deactivation.
func (s *SQLite) DeleteEndpoint(ctx context.Context, client, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active int
	var secret string
	err = tx.QueryRowContext(ctx, "SELECT active,secret FROM endpoints WHERE id=? AND client_id=?", id, client).Scan(&active, &secret)
	if errors.Is(err, sql.ErrNoRows) || err == nil && active == 0 {
		return core.Failure(core.NotFound, "endpoint not found")
	}
	if err != nil {
		return err
	}
	if secret == "" {
		return core.Failure(core.Conflict, "endpoint is managed by configuration; remove it from configuration and restart")
	}
	var unfinished int
	err = tx.QueryRowContext(ctx, "SELECT count(*) FROM deliveries WHERE endpoint_id=? AND status IN ('PENDING','RETRYING','PROCESSING')", id).Scan(&unfinished)
	if err != nil {
		return err
	}
	if unfinished > 0 {
		return core.Failure(core.Conflict, "endpoint has unfinished deliveries")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE endpoints SET active=0 WHERE id=? AND client_id=?", id, client); err != nil {
		return err
	}
	return tx.Commit()
}

// StoredEndpoint is used only after a claim. A successful deletion cannot race an
// unfinished delivery, so its destination remains available through finalization.
func (s *SQLite) StoredEndpoint(ctx context.Context, id string) (core.StoredEndpoint, error) {
	var ep core.StoredEndpoint
	err := s.reader().QueryRowContext(ctx, "SELECT id,url,secret,allow_loopback FROM endpoints WHERE id=? AND active=1 AND secret!=''", id).Scan(&ep.ID, &ep.URL, &ep.Secret, &ep.AllowLoopback)
	if errors.Is(err, sql.ErrNoRows) {
		err = core.Failure(core.NotFound, "endpoint not found")
	}
	return ep, err
}

func (s *SQLite) StoredEndpoints(ctx context.Context) ([]core.StoredEndpoint, error) {
	rows, err := s.reader().QueryContext(ctx, "SELECT id, url, secret, allow_loopback FROM endpoints WHERE active=1 AND secret!=''")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []core.StoredEndpoint{}
	for rows.Next() {
		var ep core.StoredEndpoint
		var loop int
		if err = rows.Scan(&ep.ID, &ep.URL, &ep.Secret, &loop); err != nil {
			return nil, err
		}
		ep.AllowLoopback = (loop == 1)
		out = append(out, ep)
	}
	return out, rows.Err()
}
