// receiver is a loopback-only fault-injection tool, not a production consumer.
package main

import (
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"flag"
	"hookrelay/internal/core"
	"hookrelay/internal/destination"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	addr := flag.String("listen", "127.0.0.1:9090", "loopback address")
	mode := flag.String("mode", "normal", "normal, slow, 503, 429, drop")
	first := flag.Int64("fail-first", 1, "apply failure mode to first N verified requests")
	delay := flag.Duration("delay", 7*time.Second, "response delay in slow mode")
	ledger := flag.String("ledger", "", "optional JSONL receipt log (contains identifiers, not payloads)")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		log.Fatal("listen must use a literal loopback IP")
	}
	if (*mode != "normal" && *mode != "slow" && *mode != "503" && *mode != "429" && *mode != "drop") || *first < 0 || *delay < 0 || *delay > time.Minute {
		log.Fatal("invalid fault configuration")
	}
	secret := os.Getenv("HOOKRELAY_SECRET")
	if len(secret) < 16 {
		log.Fatal("HOOKRELAY_SECRET must contain at least 16 bytes")
	}
	var file *os.File
	if *ledger != "" {
		file, err = os.OpenFile(*ledger, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
	}
	var mu sync.Mutex
	var sequence atomic.Int64
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	m.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, core.MaxBody))
		stamp := r.Header.Get("X-Webhook-Timestamp")
		t, e := strconv.ParseInt(stamp, 10, 64)
		sig, e2 := hex.DecodeString(r.Header.Get("X-Webhook-Signature"))
		expected, _ := hex.DecodeString(destination.Signature(secret, stamp, body))
		if err != nil || e != nil || e2 != nil || t < time.Now().Unix()-300 || t > time.Now().Unix()+300 || !hmac.Equal(sig, expected) {
			http.Error(w, "invalid signature", 401)
			return
		}
		n := sequence.Add(1)
		active := *mode
		if n > *first {
			active = "normal"
		}
		processed := active != "503" && active != "429"
		// Persist the simulated business side effect before delaying or losing its response.
		if file != nil {
			mu.Lock()
			err = json.NewEncoder(file).Encode(map[string]any{"sequence": n, "delivery_id": r.Header.Get("X-Webhook-Delivery-Id"), "event_id": r.Header.Get("X-Webhook-Id"), "mode": active, "processed": processed, "received_unix_ms": time.Now().UnixMilli()})
			if err == nil {
				err = file.Sync()
			}
			mu.Unlock()
			if err != nil {
				http.Error(w, "ledger unavailable", 500)
				return
			}
		}
		log.Printf("verified delivery=%s event=%s mode=%s processed=%t", r.Header.Get("X-Webhook-Delivery-Id"), r.Header.Get("X-Webhook-Id"), active, processed)
		switch active {
		case "503":
			w.WriteHeader(503)
		case "429":
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
		case "slow":
			select {
			case <-time.After(*delay):
				w.WriteHeader(204)
			case <-r.Context().Done():
				return
			}
		case "drop":
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				http.Error(w, "cannot drop response", 500)
				return
			}
			conn.Close()
		default:
			w.WriteHeader(204)
		}
	})
	srv := &http.Server{Addr: *addr, Handler: m, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 65 * time.Second, IdleTimeout: 30 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
