package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"hookrelay/internal/relay"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
		slog.Error("dispatcher stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("dispatcher", flag.ContinueOnError)
	dbPath := flags.String("db", "hookrelay.db", "SQLite path")
	addr := flags.String("listen", "127.0.0.1:8080", "API address")
	endpoint := flags.String("endpoint", "http://127.0.0.1:9090/webhook", "fixed test destination")
	allow := flags.Bool("allow-loopback-test", false, "explicitly allow test receiver on loopback")
	allowRemoteListen := flags.Bool("insecure-allow-remote-listen", false, "allow binding API to non-loopback interfaces (e.g. 0.0.0.0 in Docker/containers)")
	retryAfterMax := flags.Duration("retry-after-max", relay.DefaultRetryAfterMax, "maximum Retry-After wait (positive, at most 24h)")
	configPath := flags.String("config", "", "client/endpoint JSON config; empty uses legacy local test settings")
	limits := relay.DefaultLimits()
	flags.IntVar(&limits.Endpoints, "max-endpoints", limits.Endpoints, "maximum retained endpoints globally")
	flags.IntVar(&limits.ClientEndpoints, "max-client-endpoints", limits.ClientEndpoints, "maximum retained endpoints per client")
	flags.IntVar(&limits.Queue, "max-queue", limits.Queue, "maximum global unfinished deliveries")
	flags.IntVar(&limits.ClientQueue, "max-client-queue", limits.ClientQueue, "maximum unfinished deliveries per client")
	flags.IntVar(&limits.Events, "max-events", limits.Events, "maximum retained events")
	flags.Int64Var(&limits.PayloadBytes, "max-payload-bytes", limits.PayloadBytes, "maximum retained payload bytes")
	flags.IntVar(&limits.Cycles, "max-cycles", limits.Cycles, "maximum cycles per delivery, including initial send")
	flags.IntVar(&limits.MaxAttempts, "max-attempts", limits.MaxAttempts, "maximum attempts per delivery cycle")
	flags.DurationVar(&limits.SuccessRetention, "success-retention", limits.SuccessRetention, "retention after SUCCESS")
	flags.DurationVar(&limits.DeadRetention, "dead-retention", limits.DeadRetention, "retention after DEAD")
	flags.DurationVar(&limits.CleanupInterval, "cleanup-interval", limits.CleanupInterval, "interval between cleanup batches")
	flags.IntVar(&limits.CleanupBatch, "cleanup-batch", limits.CleanupBatch, "maximum events removed per cleanup transaction")
	workers := flags.Int("workers", 1, "number of concurrent dispatch workers (1..50)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := limits.Validate(); err != nil {
		return fmt.Errorf("invalid limits: %w", err)
	}
	if *workers < 1 || *workers > 50 {
		return errors.New("workers must be between 1 and 50")
	}
	if *retryAfterMax <= 0 || *retryAfterMax > 24*time.Hour {
		return errors.New("retry-after-max must be positive and at most 24h")
	}
	var cfg relay.Config
	var err error
	if *configPath != "" {
		cfg, err = relay.ReadConfig(*configPath)
		if err != nil {
			return errors.New("cannot read config")
		}
	} else {
		cfg = relay.Config{Clients: []relay.ClientConfig{{ID: "local-client", TokenEnv: "HOOKRELAY_TOKEN", Endpoints: []relay.EndpointConfig{{ID: "test", URL: *endpoint, SecretEnv: "HOOKRELAY_SECRET", AllowLoopback: *allow}}}}}
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return fmt.Errorf("invalid API listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return errors.New("API listen host must be a valid IP address")
	}
	if !ip.IsLoopback() {
		if !*allowRemoteListen {
			return errors.New("API must bind a literal loopback address (pass --insecure-allow-remote-listen to allow non-loopback interfaces without TLS)")
		}
		slog.Warn("API bound to non-loopback address without built-in TLS; ensure traffic is protected by reverse proxy or private network", "address", *addr)
	}
	syscall.Umask(0077) // Local database files contain event payloads.
	db, readDB, err := relay.OpenPool(*dbPath)
	if err != nil {
		return fmt.Errorf("open storage failed: %w", err)
	}
	defer db.Close()
	defer readDB.Close()
	metricsToken := os.Getenv("HOOKRELAY_METRICS_TOKEN")
	if metricsToken != "" && (len(metricsToken) < 16 || len(metricsToken) > 4096) {
		return errors.New("metrics token must contain 16..4096 bytes")
	}
	s := &relay.Service{DB: db, ReadDB: readDB, RetryAfterMax: *retryAfterMax, Limits: limits, Workers: *workers, MetricsToken: metricsToken, AllowLoopbackEndpoints: *allow}
	if err = s.ApplyConfig(cfg, os.Getenv); err != nil {
		return fmt.Errorf("invalid client/endpoint configuration: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err = s.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize endpoints: %w", err)
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen API: %w", err)
	}
	defer listener.Close()
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	workerDone := make(chan error, 1)
	serverDone := make(chan error, 1)
	go func() { workerDone <- s.Run(ctx) }()
	go func() { serverDone <- srv.Serve(listener) }()
	slog.Info("HookRelay listening", "address", listener.Addr().String())
	var runErr error
	workerRead, serverRead := false, false
	select {
	case <-ctx.Done():
	case runErr = <-workerDone:
		workerRead = true
	case err = <-serverDone:
		serverRead = true
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownErr := srv.Shutdown(shutdown)
	if shutdownErr != nil {
		srv.Close()
	}
	// Run joins both dispatch and maintenance before either database pool is closed.
	if !workerRead {
		runErr = errors.Join(runErr, <-workerDone)
	}
	if !serverRead {
		err = <-serverDone
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = errors.Join(runErr, err)
		}
	}
	return errors.Join(runErr, shutdownErr)
}
