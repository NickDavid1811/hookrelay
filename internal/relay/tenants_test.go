package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func call(s *Service, token, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Idempotency-Key", "shared-key")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestClientIsolationAndPersistence(t *testing.T) {
	s, path := setup(t)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Webhook-Signature") != Signature("secret-A-123456789", r.Header.Get("X-Webhook-Timestamp"), []byte(`{"a":1}`)) {
			t.Error("wrong endpoint secret")
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	cfg := Config{Clients: []ClientConfig{{ID: "A", TokenEnv: "TOKEN_A", Endpoints: []EndpointConfig{{ID: "ep-A", URL: receiver.URL, SecretEnv: "SECRET_A", AllowLoopback: true}}}, {ID: "B", TokenEnv: "TOKEN_B", Endpoints: []EndpointConfig{{ID: "ep-B", URL: receiver.URL, SecretEnv: "SECRET_B", AllowLoopback: true}}}}}
	env := map[string]string{"TOKEN_A": "token-A-123456789", "TOKEN_B": "token-B-123456789", "SECRET_A": "secret-A-123456789", "SECRET_B": "secret-B-123456789"}
	if err := s.ApplyConfig(cfg, func(key string) string { return env[key] }); err != nil {
		t.Fatal(err)
	}
	pubA := `{"endpoint_id":"ep-A","event_type":"test","payload":{"a":1}}`
	pubB := strings.Replace(pubA, "ep-A", "ep-B", 1)
	w := call(s, env["TOKEN_A"], "POST", "/api/v1/publish", pubA)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	var result Result
	json.Unmarshal(w.Body.Bytes(), &result)
	if call(s, env["TOKEN_B"], "POST", "/api/v1/publish", pubA).Code != 404 {
		t.Fatal("cross-owner publication")
	}
	if call(s, env["TOKEN_B"], "POST", "/api/v1/publish", pubB).Code != 202 {
		t.Fatal("idempotency not scoped")
	}
	for _, suffix := range []string{"", "/attempts", "/replays", "/replay"} {
		method := "GET"
		data := ""
		if suffix == "/replay" {
			method = "POST"
			data = `{"expected_cycle":1}`
		}
		if call(s, env["TOKEN_B"], method, "/api/v1/deliveries/"+result.DeliveryID+suffix, data).Code != 404 {
			t.Fatal("cross-client access", suffix)
		}
	}
	listing := call(s, env["TOKEN_A"], "GET", "/api/v1/endpoints", "")
	if strings.Contains(listing.Body.String(), "ep-B") || strings.Contains(listing.Body.String(), env["SECRET_A"]) {
		t.Fatal("endpoint listing leaked")
	}
	if _, err := s.DB.Exec("UPDATE deliveries SET endpoint_id='ep-B' WHERE id=?", result.DeliveryID); err == nil {
		t.Fatal("storage allowed cross-owner endpoint")
	}
	if _, err := s.DB.Exec("UPDATE events SET client_id='B' WHERE id=?", result.EventID); err == nil {
		t.Fatal("storage allowed event owner change")
	}
	if _, err := s.DB.Exec("UPDATE endpoints SET client_id='B' WHERE id='ep-A'"); err == nil {
		t.Fatal("storage allowed endpoint owner change")
	}
	if err := s.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE deliveries SET status='DEAD' WHERE id=?", result.DeliveryID); err != nil {
		t.Fatal(err)
	}
	if w := call(s, env["TOKEN_A"], "POST", "/api/v1/deliveries/"+result.DeliveryID+"/replay", `{"expected_cycle":1}`); w.Code != 202 {
		t.Fatal(w.Code, w.Body)
	}
	var actor string
	s.DB.QueryRow("SELECT actor FROM replays WHERE delivery_id=?", result.DeliveryID).Scan(&actor)
	if actor != "A" {
		t.Fatal("incorrect audit actor", actor)
	}
	s.DB.Close()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.DB = db
	defer db.Close()
	if err = s.ApplyConfig(cfg, func(key string) string { return env[key] }); err != nil {
		t.Fatal(err)
	}
	if w := call(s, env["TOKEN_A"], "GET", "/api/v1/deliveries/"+result.DeliveryID, ""); w.Code != 200 {
		t.Fatal("ownership lost on reopen")
	}
	old := cfg.Clients[0].Endpoints[0].URL
	cfg.Clients[0].Endpoints[0].URL = "http://127.0.0.1:1"
	if s.ApplyConfig(cfg, func(key string) string { return env[key] }) == nil {
		t.Fatal("changed immutable URL")
	}
	cfg.Clients[0].Endpoints[0].URL = old
	if call(s, env["TOKEN_A"], "GET", "/api/v1/endpoints", "").Code != 200 {
		t.Fatal("failed config partially disabled client")
	}
	cfg.Clients = cfg.Clients[:1]
	if err = s.ApplyConfig(cfg, func(key string) string { return env[key] }); err != nil {
		t.Fatal(err)
	}
	if call(s, env["TOKEN_B"], "GET", "/api/v1/endpoints", "").Code != 401 {
		t.Fatal("omitted client still active")
	}
	if err = s.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var attempts int
	s.DB.QueryRow("SELECT attempts FROM deliveries d JOIN events e ON e.id=d.event_id WHERE e.client_id='B'").Scan(&attempts)
	if attempts != 0 {
		t.Fatal("disabled endpoint consumed work")
	}
	env["TOKEN_A"] = "rotated-A-123456789"
	if err = s.ApplyConfig(cfg, func(key string) string { return env[key] }); err != nil {
		t.Fatal(err)
	}
	if call(s, "token-A-123456789", "GET", "/api/v1/endpoints", "").Code != 401 {
		t.Fatal("old token not revoked")
	}
}

func TestInvalidConfigIsAtomic(t *testing.T) {
	s, _ := setup(t)
	valid := ClientConfig{ID: "new", TokenEnv: "NEW_TOKEN", Endpoints: []EndpointConfig{{ID: "new-ep", URL: "http://127.0.0.1:9090", SecretEnv: "NEW_SECRET", AllowLoopback: true}}}
	env := func(string) string { return "same-value-123456789" }
	duplicate := valid
	duplicate.ID = "other"
	duplicate.Endpoints = []EndpointConfig{{ID: "other-ep", URL: valid.Endpoints[0].URL, SecretEnv: "NEW_SECRET", AllowLoopback: true}}
	if s.ApplyConfig(Config{Clients: []ClientConfig{valid, duplicate}}, env) == nil {
		t.Fatal("duplicate token allowed")
	}
	valid.Endpoints[0].AllowLoopback = false
	if s.ApplyConfig(Config{Clients: []ClientConfig{valid}}, env) == nil {
		t.Fatal("private destination allowed")
	}
	var n int
	s.DB.QueryRow("SELECT count(*) FROM clients WHERE active=1").Scan(&n)
	if n != 1 {
		t.Fatal("invalid config mutated active clients")
	}
	if call(s, testToken, "GET", "/api/v1/endpoints", "").Code != 200 {
		t.Fatal("old token revoked by invalid config")
	}
}
