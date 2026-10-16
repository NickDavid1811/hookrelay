// Package playground is a local lab adapter. It consumes the dispatcher's HTTP API;
// it never queries SQLite or calls application use cases directly.
package playground

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hookrelay/internal/core"
	"hookrelay/internal/destination"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

type Scenario struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

var Scenarios = []Scenario{
	{"success", "Entrega correcta", "El receptor verifica HMAC y responde 204."},
	{"slow", "Receptor lento", "El receptor tarda 1,5 s; el único worker queda ocupado."},
	{"temporary", "Fallo temporal", "Dos respuestas 503, backoff y recuperación."},
	{"rate_limit", "Límite de tasa", "429 con Retry-After de 3 s y posterior 204."},
	{"drop", "Respuesta perdida", "Se procesa el mensaje, se pierde la respuesta y aparece un duplicado."},
	{"exhausted", "Intentos agotados", "Cinco respuestas 503 llevan a DEAD; puedes restablecer y reenviar."},
}

func validScenario(id string) bool {
	for _, s := range Scenarios {
		if s.ID == id {
			return true
		}
	}
	return false
}

type Event struct {
	At     int64  `json:"at"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Source string `json:"source"`
}
type Receipt struct {
	At        int64  `json:"at"`
	Attempt   int    `json:"attempt"`
	Processed bool   `json:"processed"`
	Phase     string `json:"phase"`
	Code      int    `json:"code"`
}
type Run struct {
	ID           string           `json:"id"`
	EventID      string           `json:"event_id"`
	Scenario     string           `json:"scenario"`
	EventType    string           `json:"event_type"`
	Created      int64            `json:"created"`
	Status       string           `json:"status"`
	Cycle        int              `json:"cycle"`
	Attempts     int              `json:"attempts"`
	HTTPStatus   int              `json:"http_status"`
	Error        string           `json:"error,omitempty"`
	Observed     int64            `json:"observed_at"`
	NextEstimate int64            `json:"next_estimate,omitempty"`
	History      []map[string]any `json:"history"`
	Receipts     []Receipt        `json:"receipts"`
	Timeline     []Event          `json:"timeline"`
	recover      bool
	busy         bool
	lastStatus   string
}
type Stats struct {
	Queue      float64 `json:"queue"`
	QueueLimit float64 `json:"queue_limit"`
	Oldest     float64 `json:"oldest"`
	Events     float64 `json:"events"`
	Bytes      float64 `json:"bytes"`
	Success    float64 `json:"success"`
	Dead       float64 `json:"dead"`
	Worker     float64 `json:"worker"`
}
type Snapshot struct {
	Runs      []Run      `json:"runs"`
	Scenarios []Scenario `json:"scenarios"`
	Stats     Stats      `json:"stats"`
	Observed  int64      `json:"observed_at"`
	Error     string     `json:"error,omitempty"`
}
type Lab struct {
	mu                               sync.RWMutex
	api, token, metricsToken, secret string
	client                           *http.Client
	runs                             map[string]*Run
	order                            []string
	stats                            Stats
	observed                         int64
	lastError                        string
	publishing                       bool
}

func New(api, token, metricsToken, secret string) *Lab {
	return &Lab{api: api, token: token, metricsToken: metricsToken, secret: secret, client: &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, runs: map[string]*Run{}}
}
func (l *Lab) call(ctx context.Context, method, path string, body []byte, key string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, l.api+path, bytes.NewReader(body))
	if err != nil {
		return errors.New("petición interna inválida")
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return errors.New("dispatcher no disponible")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&failure)
		if len(failure.Error) > 256 {
			failure.Error = "petición rechazada"
		}
		return fmt.Errorf("HookRelay HTTP %d: %s", resp.StatusCode, failure.Error)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
func (l *Lab) Publish(ctx context.Context, scenario, eventType string, payload json.RawMessage, count int) ([]string, error) {
	if !validScenario(scenario) || len(eventType) == 0 || len(eventType) > 128 || len(payload) == 0 || len(payload) > 4096 || !json.Valid(payload) || count < 1 || count > 12 {
		return nil, errors.New("escenario, tipo o payload inválido (máximo 4 KiB y 12 eventos)")
	}
	l.mu.Lock()
	if l.publishing || len(l.order)+count > 200 {
		l.mu.Unlock()
		return nil, errors.New("laboratorio ocupado o límite de 200 eventos de sesión alcanzado")
	}
	l.publishing = true
	l.mu.Unlock()
	defer func() { l.mu.Lock(); l.publishing = false; l.mu.Unlock() }()
	ids := []string{}
	for i := 0; i < count; i++ {
		body, _ := json.Marshal(core.Publication{EndpointID: "lab-" + scenario, EventType: eventType, Payload: payload})
		var result core.Result
		if err := l.call(ctx, "POST", "/api/v1/publish", body, core.ID(), &result); err != nil {
			return ids, err
		}
		now := time.Now().UnixMilli()
		run := &Run{ID: result.DeliveryID, EventID: result.EventID, Scenario: scenario, EventType: eventType, Created: now, Status: result.Status, Cycle: result.Cycle, Observed: now, History: []map[string]any{}, Receipts: []Receipt{}, Timeline: []Event{{now, "accepted", "Publicación aceptada", "HTTP 202: evento y entrega persistidos atómicamente.", "api"}}}
		l.mu.Lock()
		// Receiver may arrive before the publication response. Preserve its receipt evidence.
		if early := l.runs[run.ID]; early != nil {
			run.Receipts = early.Receipts
			run.Timeline = append(run.Timeline, early.Timeline...)
		}
		l.runs[run.ID] = run
		l.order = append(l.order, run.ID)
		l.mu.Unlock()
		ids = append(ids, run.ID)
	}
	return ids, nil
}
func (l *Lab) Replay(ctx context.Context, id string, cycle int) error {
	l.mu.Lock()
	run := l.runs[id]
	if run == nil || run.ID == "" {
		l.mu.Unlock()
		return errors.New("entrega desconocida")
	}
	if run.Status != "DEAD" || run.Cycle != cycle || run.busy {
		l.mu.Unlock()
		return errors.New("se requiere DEAD y el ciclo actual")
	}
	run.busy = true
	previous := run.recover
	run.recover = true
	l.mu.Unlock()
	body, _ := json.Marshal(map[string]any{"expected_cycle": cycle, "reason": "Playground: restablecer receptor y reenviar"})
	var result core.Result
	err := l.call(ctx, "POST", "/api/v1/deliveries/"+id+"/replay", body, "", &result)
	l.mu.Lock()
	defer l.mu.Unlock()
	run.busy = false
	if err != nil {
		run.recover = previous
		return err
	}
	run.Status = result.Status
	run.Cycle = result.Cycle
	run.Error = ""
	run.NextEstimate = 0
	run.Timeline = appendBounded(run.Timeline, Event{time.Now().UnixMilli(), "replay", "Nuevo ciclo autorizado", "Receptor restablecido a 204; historial conservado.", "api"})
	return nil
}
func appendBounded(events []Event, event Event) []Event {
	events = append(events, event)
	if len(events) > 96 {
		events = events[len(events)-96:]
	}
	return events
}
func (l *Lab) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := Snapshot{Runs: []Run{}, Scenarios: Scenarios, Stats: l.stats, Observed: l.observed, Error: l.lastError}
	for i := len(l.order) - 1; i >= 0; i-- {
		r := *l.runs[l.order[i]]
		r.Receipts = append([]Receipt{}, r.Receipts...)
		r.Timeline = append([]Event{}, r.Timeline...)
		r.History = append([]map[string]any{}, r.History...)
		out.Runs = append(out.Runs, r)
	}
	return out
}
func (l *Lab) Poll(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		l.refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (l *Lab) refresh(ctx context.Context) {
	l.mu.RLock()
	ids := append([]string{}, l.order...)
	l.mu.RUnlock()
	lastError := ""
	for _, id := range ids {
		l.mu.RLock()
		r := l.runs[id]
		terminal := r.Status == "SUCCESS" || r.Status == "DEAD"
		busy := r.busy
		l.mu.RUnlock()
		if terminal || busy {
			continue
		}
		var result core.Result
		if err := l.call(ctx, "GET", "/api/v1/deliveries/"+id, nil, "", &result); err != nil {
			lastError = "No se pudo actualizar una entrega"
			continue
		}
		var history []map[string]any
		if err := l.call(ctx, "GET", "/api/v1/deliveries/"+id+"/attempts", nil, "", &history); err != nil {
			lastError = "No se pudo actualizar el historial"
			continue
		}
		now := time.Now().UnixMilli()
		l.mu.Lock()
		run := l.runs[id]
		if !run.busy && result.Cycle >= run.Cycle {
			if run.Status != result.Status {
				run.Timeline = appendBounded(run.Timeline, Event{now, "state", result.Status, "Estado observado mediante la API de HookRelay.", "observation"})
			}
			if len(history) > len(run.History) {
				for _, attempt := range history[len(run.History):] {
					code, _ := attempt["http_status"].(float64)
					number, _ := attempt["number"].(float64)
					run.Timeline = appendBounded(run.Timeline, Event{now, "attempt", fmt.Sprintf("Intento %.0f · HTTP %.0f", number, code), "Resultado confirmado en el historial persistido.", "api"})
				}
			}
			run.Status = result.Status
			run.Cycle = result.Cycle
			run.Attempts = result.Attempts
			run.HTTPStatus = result.HTTPStatus
			run.Error = result.Error
			run.History = history
			run.Observed = now
			// Only the controlled 429 scenario has an observable explicit lower bound.
			run.NextEstimate = 0
			if result.Status == "RETRYING" && run.Scenario == "rate_limit" && len(run.Receipts) > 0 {
				run.NextEstimate = run.Receipts[0].At + 3000
			}
		}
		l.mu.Unlock()
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", l.api+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+l.metricsToken)
	resp, err := l.client.Do(req)
	if err == nil {
		if resp.StatusCode == 200 {
			parser := expfmt.NewTextParser(model.LegacyValidation)
			families, parseErr := parser.TextToMetricFamilies(io.LimitReader(resp.Body, 1<<20))
			if parseErr == nil {
				gauge := func(name string) float64 {
					family := families[name]
					if family != nil && len(family.Metric) > 0 {
						return family.Metric[0].GetGauge().GetValue()
					}
					return 0
				}
				stats := Stats{gauge("hookrelay_queue_depth"), gauge("hookrelay_queue_limit"), gauge("hookrelay_queue_oldest_age_seconds"), gauge("hookrelay_events_retained"), gauge("hookrelay_payload_bytes_retained"), 0, 0, gauge("hookrelay_worker_running")}
				if family := families["hookrelay_deliveries"]; family != nil {
					for _, m := range family.Metric {
						for _, label := range m.Label {
							if label.GetName() == "state" {
								if label.GetValue() == "SUCCESS" {
									stats.Success = m.GetGauge().GetValue()
								}
								if label.GetValue() == "DEAD" {
									stats.Dead = m.GetGauge().GetValue()
								}
							}
						}
					}
				}
				l.mu.Lock()
				l.stats = stats
				l.mu.Unlock()
			} else {
				lastError = "Métricas no disponibles"
			}
		} else {
			lastError = "Métricas no disponibles"
		}
		resp.Body.Close()
	} else {
		lastError = "Dispatcher sin conexión"
	}
	l.mu.Lock()
	l.observed = time.Now().UnixMilli()
	l.lastError = lastError
	l.mu.Unlock()
}
func (l *Lab) Receiver() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		scenario := r.PathValue("scenario")
		if scenario == "" {
			scenario = strings.TrimPrefix(r.URL.Path, "/receiver/")
		}
		if !validScenario(scenario) {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, core.MaxBody))
		stamp := r.Header.Get("X-Webhook-Timestamp")
		sec, e := strconv.ParseInt(stamp, 10, 64)
		sig, e2 := hex.DecodeString(r.Header.Get("X-Webhook-Signature"))
		expected, _ := hex.DecodeString(destination.Signature(l.secret, stamp, body))
		id := r.Header.Get("X-Webhook-Delivery-Id")
		if err != nil || e != nil || e2 != nil || sec < time.Now().Unix()-300 || sec > time.Now().Unix()+300 || len(id) != 32 || !hmac.Equal(sig, expected) {
			http.Error(w, "invalid signature", 401)
			return
		}
		now := time.Now().UnixMilli()
		l.mu.Lock()
		run := l.runs[id]
		if run == nil {
			if len(l.runs) >= 220 {
				l.mu.Unlock()
				http.Error(w, "lab capacity", 503)
				return
			}
			run = &Run{Scenario: scenario, Receipts: []Receipt{}, History: []map[string]any{}, Timeline: []Event{}}
			l.runs[id] = run
		}
		number := len(run.Receipts) + 1
		mode := scenario
		if run.recover {
			mode = "success"
		}
		code := 204
		processed := true
		if (mode == "temporary" && number <= 2) || mode == "exhausted" {
			code = 503
			processed = false
		}
		if mode == "rate_limit" && number == 1 {
			code = 429
			processed = false
		}
		lost := mode == "drop" && number == 1
		if len(run.Receipts) >= 64 {
			l.mu.Unlock()
			http.Error(w, "receipt limit", 503)
			return
		}
		index := len(run.Receipts)
		run.Receipts = append(run.Receipts, Receipt{now, number, processed, "received", 0})
		detail := "HMAC verificado; mensaje aceptado por el receptor simulado."
		if !processed {
			detail = "HMAC verificado; el receptor rechazó temporalmente el mensaje."
		}
		run.Timeline = appendBounded(run.Timeline, Event{now, "received", "Receptor: mensaje recibido", detail, "receiver"})
		l.mu.Unlock()
		finish := func(phase string, response int) {
			l.mu.Lock()
			defer l.mu.Unlock()
			run := l.runs[id]
			if index < len(run.Receipts) {
				run.Receipts[index].Phase = phase
				run.Receipts[index].Code = response
			}
			run.Timeline = appendBounded(run.Timeline, Event{time.Now().UnixMilli(), phase, "Receptor: " + phase, fmt.Sprintf("HTTP %d · procesamiento simulado: %t", response, processed), "receiver"})
		}
		if lost {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				finish("error", 0)
				return
			}
			finish("respuesta perdida", 0)
			conn.Close()
			return
		}
		if mode == "slow" {
			select {
			case <-time.After(1500 * time.Millisecond):
			case <-r.Context().Done():
				finish("conexión cancelada", 0)
				return
			}
		}
		if code == 429 {
			w.Header().Set("Retry-After", "3")
		}
		w.WriteHeader(code)
		finish("respuesta emitida", code)
	})
}
