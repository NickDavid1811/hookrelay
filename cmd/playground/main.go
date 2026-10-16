// playground composes a separate local lab and accesses HookRelay through HTTP.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"hookrelay/internal/playground"
	"hookrelay/internal/relay"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

func loopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	return err == nil && net.ParseIP(host).IsLoopback()
}
func main() {
	if err := run(); err != nil {
		slog.Error("playground detenido", "error", err)
		os.Exit(1)
	}
}
func run() error {
	addr := flag.String("listen", "127.0.0.1:8090", "loopback UI address")
	receiverAddr := flag.String("receiver-listen", "127.0.0.1:9091", "loopback receiver address; retain across restarts")
	dataDir := flag.String("data-dir", "work/playground", "dedicated laboratory data, never the production database")
	webDir := flag.String("web-dir", "web/playground/dist", "built frontend directory")
	flag.Parse()
	if !loopback(*addr) || !loopback(*receiverAddr) {
		return errors.New("UI y receptor requieren una IP literal de loopback")
	}
	if _, err := os.Stat(filepath.Join(*webDir, "index.html")); err != nil {
		return errors.New("frontend no compilado: ejecuta make web-build")
	}
	syscall.Umask(0077)
	if err := os.MkdirAll(*dataDir, 0700); err != nil {
		return err
	}
	ui, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	defer ui.Close()
	receiver, err := net.Listen("tcp", *receiverAddr)
	if err != nil {
		return err
	}
	defer receiver.Close()
	apiListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer apiListener.Close()
	db, err := relay.Open(filepath.Join(*dataDir, "lab.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	token, metricsToken, secret := relay.ID()+relay.ID(), relay.ID()+relay.ID(), relay.ID()+relay.ID()
	config := relay.Config{Clients: []relay.ClientConfig{{ID: "playground", TokenEnv: "LAB_TOKEN", Endpoints: []relay.EndpointConfig{}}}}
	for _, scenario := range playground.Scenarios {
		config.Clients[0].Endpoints = append(config.Clients[0].Endpoints, relay.EndpointConfig{ID: "lab-" + scenario.ID, URL: "http://" + receiver.Addr().String() + "/receiver/" + scenario.ID, SecretEnv: "LAB_SECRET", AllowLoopback: true})
	}
	limits := relay.DefaultLimits()
	limits.Queue = 200
	limits.ClientQueue = 200
	limits.Events = 1000
	limits.PayloadBytes = 4 << 20
	limits.Cycles = 5
	service := &relay.Service{DB: db, AllowLoopbackEndpoints: true, MetricsToken: metricsToken, Limits: limits}
	if err = service.ApplyConfig(config, func(name string) string {
		if name == "LAB_TOKEN" {
			return token
		}
		return secret
	}); err != nil {
		return fmt.Errorf("configuración del laboratorio: %w (conserva receiver-listen o usa otro data-dir)", err)
	}
	if err = service.Initialize(context.Background()); err != nil {
		return err
	}
	lab := playground.New("http://"+apiListener.Addr().String(), token, metricsToken, secret)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	servers := []*http.Server{
		{Handler: lab.Handler(ui.Addr().String(), http.FileServer(http.Dir(*webDir))), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192},
		{Handler: service.Handler(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192},
		{Handler: lab.Receiver(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192},
	}
	listeners := []net.Listener{ui, apiListener, receiver}
	errCh := make(chan error, 4)
	var wg sync.WaitGroup
	for i, server := range servers {
		wg.Add(1)
		go func(server *http.Server, listener net.Listener) {
			defer wg.Done()
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- err
			}
		}(server, listeners[i])
	}
	wg.Add(2)
	go func() { defer wg.Done(); errCh <- service.Run(ctx) }()
	go func() { defer wg.Done(); lab.Poll(ctx) }()
	slog.Info("Flow Lab listo", "url", "http://"+ui.Addr().String(), "modo", "local, datos aislados")
	select {
	case <-ctx.Done():
	case err = <-errCh:
		stop()
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, server := range servers {
		server.Shutdown(shutdown)
		server.Close()
	}
	wg.Wait()
	return err
}
