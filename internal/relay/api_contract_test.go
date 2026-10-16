package relay

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Exercise documented requests through the real HTTP adapter and SQLite use cases.
func TestDocumentedRequests(t *testing.T) {
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type    string `yaml:"type"`
					Example any    `yaml:"example"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	raw, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err = yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	properties := spec.Components.Schemas["CreateEndpointRequest"].Properties
	if _, ok := properties["id"]; !ok {
		t.Fatal("OpenAPI must document id")
	}
	if payload := spec.Components.Schemas["PublicationRequest"].Properties["payload"]; payload.Type != "" {
		t.Fatal("OpenAPI restricts a payload type accepted by the backend")
	}
	s, _ := setup(t)
	for _, field := range []string{"id", "endpoint_id"} {
		req := httptest.NewRequest("POST", "/api/v1/endpoints", strings.NewReader(fmt.Sprintf(`{"%s":%q,"url":%q}`, field, properties["id"].Example, properties["url"].Example)))
		req.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		want := 201
		if field == "endpoint_id" {
			want = 400
		}
		if w.Code != want {
			t.Fatalf("%s: %d %s", field, w.Code, w.Body)
		}
	}
	for i, payload := range []string{`{}`, `[]`, `null`, `"text"`, `1`, `true`} {
		w := publish(s, fmt.Sprintf("json-%d", i), `{"endpoint_id":"test","event_type":"test","payload":`+payload+`}`)
		if w.Code != 202 {
			t.Fatalf("payload %s: %d %s", payload, w.Code, w.Body)
		}
	}
}

func TestEndpointPaginationAndConfigurationOwnership(t *testing.T) {
	s, _ := setup(t)
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1001; i++ {
		if _, err = tx.Exec("INSERT INTO endpoints(id,client_id,url,secret_env,active,secret) VALUES(?,'local-client','https://example.com','','1','test-secret-value')", fmt.Sprintf("ep-%04d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var all []string
	after := ""
	for page := 0; page < 3; page++ {
		req := httptest.NewRequest("GET", "/api/v1/endpoints?after="+after, nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		var records []struct {
			ID string `json:"id"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &records); err != nil {
			t.Fatal(err)
		}
		if len(records) == 0 {
			break
		}
		for _, ep := range records {
			if ep.ID <= after {
				t.Fatal("cursor repeated or unordered", ep.ID, after)
			}
			all = append(all, ep.ID)
			after = ep.ID
		}
	}
	if len(all) != 1002 {
		t.Fatalf("lost endpoints: %d", len(all))
	}
	req := httptest.NewRequest("DELETE", "/api/v1/endpoints/test", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatal("configuration-owned endpoint deletion", w.Code, w.Body)
	}
}
