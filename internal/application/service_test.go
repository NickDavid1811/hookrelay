package application

import (
	"context"
	"errors"
	"hookrelay/internal/core"
	"reflect"
	"testing"
	"time"
)

type fakeStore struct {
	core.QueueStore // Unexpected calls fail immediately; each test supplies its exercised operations.
	publish         func(core.PublishCommand) (core.Result, bool, error)
	claim           core.Claim
	found           bool
	finalizeErr     error
	completed       *core.Completion
	order           *[]string
}

func (f *fakeStore) Publish(_ context.Context, c core.PublishCommand, _ core.Limits) (core.Result, bool, error) {
	return f.publish(c)
}
func (f *fakeStore) Claim(context.Context, int64, string) (core.Claim, bool, error) {
	return f.claim, f.found, nil
}
func (f *fakeStore) Payload(context.Context, string) ([]byte, error) {
	return []byte(`{"raw": 1}`), nil
}
func (f *fakeStore) Finalize(_ context.Context, c core.Completion) error {
	f.completed = &c
	*f.order = append(*f.order, "finalize")
	return f.finalizeErr
}

type fakeSender struct {
	result core.HTTPResult
	err    error
	sent   *core.Outbound
	order  *[]string
}

func (f *fakeSender) Send(ctx context.Context, o core.Outbound) (core.HTTPResult, error) {
	if _, ok := ctx.Deadline(); !ok {
		panic("missing delivery timeout")
	}
	f.sent = &o
	*f.order = append(*f.order, "send")
	return f.result, f.err
}

type fakeObserver struct {
	core.NopObserver
	order *[]string
}

func (o fakeObserver) HTTP(core.HTTPResult) { *o.order = append(*o.order, "http") }
func (o fakeObserver) Delivery(string)      { *o.order = append(*o.order, "delivery") }
func (o fakeObserver) Publication(v string) { *o.order = append(*o.order, v) }
func TestPublicationObservesOnlyAcceptedReceipts(t *testing.T) {
	for _, tc := range []struct {
		name string
		idem bool
		err  error
		want []string
	}{
		{"new", false, nil, []string{"commit", "new"}},
		{"idempotent", true, nil, []string{"commit", "idempotent"}},
		{"rollback", false, errors.New("commit failed"), []string{"commit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			order := []string{}
			store := &fakeStore{publish: func(c core.PublishCommand) (core.Result, bool, error) {
				if c.Client != "tenant" || c.Key != "key" || string(c.Publication.Payload) != `{"raw": 1}` || c.EventID == "" || c.Hash == "" {
					t.Fatalf("bad publication: %+v", c)
				}
				order = append(order, "commit")
				return core.Result{}, tc.idem, tc.err
			}}
			s := Service{Publications: store, Observer: fakeObserver{order: &order}}
			_, err := s.Publish(context.Background(), "tenant", "key", []byte(`{"endpoint_id":"endpoint","event_type":"test","payload":{"raw": 1}}`))
			if !errors.Is(err, tc.err) || !reflect.DeepEqual(order, tc.want) {
				t.Fatalf("err=%v order=%v", err, order)
			}
		})
	}
}
func TestDispatchCommitBoundary(t *testing.T) {
	for _, tc := range []struct {
		name               string
		sendErr, commitErr error
		want               []string
	}{
		{"success", nil, nil, []string{"send", "http", "finalize", "delivery"}},
		{"commit failure", nil, errors.New("lease lost"), []string{"send", "http", "finalize"}},
		{"sender failure", errors.New("missing endpoint"), nil, []string{"send"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			order := []string{}
			store := &fakeStore{claim: core.Claim{ID: "delivery", EventID: "event", EndpointID: "endpoint", Token: "lease", Cycle: 1}, found: true, finalizeErr: tc.commitErr, order: &order}
			sender := &fakeSender{result: core.HTTPResult{Code: 204}, err: tc.sendErr, order: &order}
			s := Service{Queue: store, Sender: sender, Observer: fakeObserver{order: &order}}
			found, err := s.DispatchOne(context.Background())
			wantErr := tc.sendErr
			if wantErr == nil {
				wantErr = tc.commitErr
			}
			if !found || err != wantErr {
				t.Fatalf("found=%v err=%v", found, err)
			}
			if !reflect.DeepEqual(order, tc.want) {
				t.Fatalf("order=%v", order)
			}
			if string(sender.sent.Body) != `{"raw": 1}` || sender.sent.DeliveryID != "delivery" {
				t.Fatalf("outbound=%+v", sender.sent)
			}
			if store.completed != nil && (store.completed.Status != "SUCCESS" || store.completed.Terminal == 0 || store.completed.Claim.Token != "lease") {
				t.Fatalf("completion=%+v", store.completed)
			}
		})
	}
}
func TestEmptyQueueAndCanceledRun(t *testing.T) {
	s := Service{Queue: &fakeStore{}}
	found, err := s.DispatchOne(context.Background())
	if found || err != nil {
		t.Fatal(found, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled worker did not exit")
	}
}

func TestLargeAttemptCountSchedulesSafeRetry(t *testing.T) {
	for _, attempt := range []int{34, 49} {
		order := []string{}
		st := &fakeStore{claim: core.Claim{ID: "d", Cycle: 1, CycleAttempts: attempt}, found: true, order: &order}
		sender := &fakeSender{result: core.HTTPResult{Code: 503}, order: &order}
		l := core.DefaultLimits()
		l.MaxAttempts = 50
		s := Service{Queue: st, Sender: sender, Limits: l}
		before := time.Now().UnixMilli()
		if _, err := s.DispatchOne(context.Background()); err != nil {
			t.Fatal(err)
		}
		if st.completed.Next < before || st.completed.Next > time.Now().Add(core.MaxBackoff).UnixMilli() {
			t.Fatal("retry overflow", st.completed.Next)
		}
	}
}

type blockingCleanupStore struct {
	core.QueueStore
	started, release, finished chan struct{}
}

func (st *blockingCleanupStore) Claim(context.Context, int64, string) (core.Claim, bool, error) {
	return core.Claim{}, false, nil
}
func (st *blockingCleanupStore) Cleanup(ctx context.Context, _ time.Time, _ core.Limits) (int, error) {
	close(st.started)
	<-ctx.Done()
	<-st.release
	close(st.finished)
	return 0, ctx.Err()
}
func TestRunJoinsMaintenanceBeforeReturning(t *testing.T) {
	st := &blockingCleanupStore{started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	l := core.DefaultLimits()
	l.CleanupInterval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := Service{Queue: st, Limits: l, Workers: 3}
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	select {
	case <-st.started:
	case <-time.After(time.Second):
		t.Fatal("cleanup not started")
	}
	cancel()
	select {
	case err := <-done:
		close(st.release)
		t.Fatalf("returned while maintenance still owns resources: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(st.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not terminate")
	}
	select {
	case <-st.finished:
	default:
		t.Fatal("cleanup not joined")
	}
}
