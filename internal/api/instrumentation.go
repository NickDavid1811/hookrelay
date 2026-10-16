package api

import (
	"net/http"
	"strings"
)

func businessOperation(r *http.Request) string {
	if r.Method != "POST" {
		return ""
	}
	if r.URL.Path == "/api/v1/publish" {
		return "publish"
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) == 6 && parts[1] == "api" && parts[2] == "v1" && parts[3] == "deliveries" && parts[4] != "" && parts[5] == "replay" {
		return "replay"
	}
	return ""
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
