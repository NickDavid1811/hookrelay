package playground

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalBoundaryAndNoCredentialLeak(t *testing.T) {
	lab := New("http://127.0.0.1:1", "business-token-private", "metrics-token-private", "hmac-secret-private")
	handler := lab.Handler("127.0.0.1:8090", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("frontend")) }))
	request := func(method, path, host, origin, marker string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:8090"+path, strings.NewReader(`{}`))
		r.Host = host
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Playground-Request", marker)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if request("GET", "/", "evil.example", "", "").Code != 403 {
		t.Fatal("DNS rebinding host allowed")
	}
	if request("GET", "/play/api/state", "127.0.0.1:8090", "https://evil.example", "").Code != 403 {
		t.Fatal("foreign origin allowed")
	}
	if request("POST", "/play/api/publish", "127.0.0.1:8090", "", "").Code != 403 {
		t.Fatal("simple cross-site mutation allowed")
	}
	if request("POST", "/play/api/publish", "127.0.0.1:8090", "http://127.0.0.1:8090", "1").Code != 400 {
		t.Fatal("invalid JSON accepted")
	}
	w := request("GET", "/play/api/export", "127.0.0.1:8090", "", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, secret := range []string{lab.token, lab.secret, lab.metricsToken, lab.api} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("export leaked private values")
		}
	}
	var snapshot Snapshot
	if json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || len(snapshot.Scenarios) != 6 {
		t.Fatal("invalid export")
	}
}
func TestSnapshotIsIndependent(t *testing.T) {
	lab := New("", "", "", "")
	lab.runs["run"] = &Run{ID: "run", Receipts: []Receipt{{Attempt: 1}}, Timeline: []Event{{Title: "first"}}, History: []map[string]any{}}
	lab.order = []string{"run"}
	snapshot := lab.Snapshot()
	snapshot.Runs[0].Receipts[0].Attempt = 99
	snapshot.Runs[0].Timeline[0].Title = "changed"
	if lab.runs["run"].Receipts[0].Attempt != 1 || lab.runs["run"].Timeline[0].Title != "first" {
		t.Fatal("mutable live slices leaked")
	}
}
