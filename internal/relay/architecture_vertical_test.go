package relay

import (
	"context"
	"encoding/json"
	"hookrelay/internal/destination"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type memoryTransport func(*http.Request) (*http.Response, error)

func (f memoryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise real HTTP handlers, use cases and SQLite with only outbound transport replaced.
// This complements the real-socket integration suite; it does not replace it.
func TestHexagonalVerticalWithoutSockets(t *testing.T) {
	s, path := setup(t)
	var calls int
	s.Endpoints["test"] = destination.Runtime{URL: "http://127.0.0.1:1", Secret: testSecret, Client: &http.Client{Transport: memoryTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(payload) != `{"invoice_id":"inv_1"}` || Signature(testSecret, r.Header.Get("X-Webhook-Timestamp"), payload) != r.Header.Get("X-Webhook-Signature") {
			t.Fatal("payload or signature changed")
		}
		return &http.Response{StatusCode: 204, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}}
	w := publish(s, "architecture", body)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	var receipt Result
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	found, err := s.dispatchOne(context.Background())
	if !found || err != nil || calls != 1 {
		t.Fatal(found, err, calls)
	}
	// Open another connection to verify the result was actually persisted.
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reopened := &Service{DB: db}
	request := func(suffix string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/deliveries/"+receipt.DeliveryID+suffix, nil)
		r.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		reopened.Handler().ServeHTTP(w, r)
		return w
	}
	w = request("")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var result Result
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Status != "SUCCESS" || result.Attempts != 1 || result.HTTPStatus != 204 {
		t.Fatalf("%+v", result)
	}
	w = request("/attempts")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"http_status":204`) {
		t.Fatal(w.Code, w.Body)
	}
	if metricValue(t, s, "hookrelay_delivery_results_total", map[string]string{"result": "SUCCESS"}) != 1 {
		t.Fatal("missing committed result metric")
	}
}
