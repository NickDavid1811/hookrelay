package playground

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func jsonReply(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(value)
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(value) != nil || dec.Decode(new(any)) != io.EOF {
		jsonReply(w, 400, map[string]string{"error": "JSON inválido o demasiado grande"})
		return false
	}
	return true
}
func (l *Lab) Handler(host string, static http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /play/api/state", func(w http.ResponseWriter, r *http.Request) { jsonReply(w, 200, l.Snapshot()) })
	mux.HandleFunc("GET /play/api/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="hookrelay-lab.json"`)
		jsonReply(w, 200, l.Snapshot())
	})
	mux.HandleFunc("POST /play/api/publish", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Scenario  string          `json:"scenario"`
			EventType string          `json:"event_type"`
			Payload   json.RawMessage `json:"payload"`
			Count     int             `json:"count"`
		}
		if !decode(w, r, &input) {
			return
		}
		ids, err := l.Publish(r.Context(), input.Scenario, input.EventType, input.Payload, input.Count)
		if err != nil {
			jsonReply(w, 400, map[string]any{"error": err.Error(), "accepted_ids": ids})
			return
		}
		jsonReply(w, 202, map[string]any{"ids": ids})
	})
	mux.HandleFunc("POST /play/api/replay/{id}", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Cycle int `json:"expected_cycle"`
		}
		if !decode(w, r, &input) {
			return
		}
		if err := l.Replay(r.Context(), r.PathValue("id"), input.Cycle); err != nil {
			jsonReply(w, 409, map[string]string{"error": err.Error()})
			return
		}
		jsonReply(w, 202, map[string]string{"status": "PENDING"})
	})
	mux.HandleFunc("/play/api/", func(w http.ResponseWriter, r *http.Request) {
		jsonReply(w, 404, map[string]string{"error": "ruta de laboratorio desconocida"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return
		}
		static.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		if r.Host != host {
			http.Error(w, "Host no autorizado", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+host {
			http.Error(w, "Origin no autorizado", 403)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "Acceso cruzado no autorizado", 403)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/play/api/") && r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("X-Playground-Request") != "1" {
			http.Error(w, "Cabecera de laboratorio requerida", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
