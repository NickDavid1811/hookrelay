package relay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hookrelay/internal/core"
	"hookrelay/internal/destination"
	"hookrelay/internal/storage"
	"io"
	"os"
	"regexp"
)

type EndpointConfig struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	SecretEnv     string `json:"secret_env"`
	AllowLoopback bool   `json:"allow_loopback_test"`
}
type ClientConfig struct {
	ID        string           `json:"id"`
	TokenEnv  string           `json:"token_env"`
	Endpoints []EndpointConfig `json:"endpoints"`
}
type Config struct {
	Clients []ClientConfig `json:"clients"`
}
type endpointRuntime = destination.Runtime

func tokenHash(token string) string { return core.TokenHash(token) }

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func ReadConfig(path string) (Config, error) {
	var cfg Config
	f, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return cfg, errors.New("config exceeds 1 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&cfg) != nil || dec.Decode(new(any)) != io.EOF {
		return cfg, errors.New("invalid config JSON")
	}
	return cfg, nil
}

// ApplyConfig reconciles active clients/endpoints atomically before the worker starts.
// Destinations and ownership are immutable once configured; omission pauses work.
func (s *Service) ApplyConfig(cfg Config, getenv func(string) string) error {
	if len(cfg.Clients) == 0 || len(cfg.Clients) > 100 {
		return errors.New("config requires 1..100 clients")
	}
	runtime := map[string]endpointRuntime{}
	hashes := map[string]bool{}
	ids := map[string]bool{}
	total := 0
	for _, c := range cfg.Clients {
		if len(c.Endpoints) > s.limits().ClientEndpoints {
			return errors.New("configured client endpoint capacity reached")
		}
		if !identifier.MatchString(c.ID) || !envName.MatchString(c.TokenEnv) || ids[c.ID] {
			return errors.New("invalid client config")
		}
		ids[c.ID] = true
		token := getenv(c.TokenEnv)
		if len(token) < 16 || len(token) > 4096 {
			return errors.New("client token must contain 16..4096 bytes")
		}
		hash := tokenHash(token)
		if s.MetricsToken != "" && hash == tokenHash(s.MetricsToken) {
			return errors.New("metrics token must differ from all client tokens")
		}
		if hashes[hash] {
			return errors.New("clients require unique tokens")
		}
		hashes[hash] = true
		for _, e := range c.Endpoints {
			total++
			if total > s.limits().Endpoints || !identifier.MatchString(e.ID) || !envName.MatchString(e.SecretEnv) {
				return errors.New("invalid endpoint config")
			}
			if _, exists := runtime[e.ID]; exists {
				return errors.New("endpoint IDs must be globally unique")
			}
			secret := getenv(e.SecretEnv)
			if len(secret) < 16 || len(secret) > 4096 {
				return errors.New("endpoint secret must contain 16..4096 bytes")
			}
			client, err := destination.Client(e.URL, e.AllowLoopback)
			if err != nil {
				return fmt.Errorf("endpoint %s: %w", e.ID, err)
			}
			runtime[e.ID] = endpointRuntime{URL: e.URL, Secret: secret, Client: client}
		}
	}
	prepared := []core.ClientSetup{}
	for _, c := range cfg.Clients {
		client := core.ClientSetup{ID: c.ID, TokenHash: tokenHash(getenv(c.TokenEnv))}
		for _, e := range c.Endpoints {
			client.Endpoints = append(client.Endpoints, core.EndpointSetup{ID: e.ID, URL: e.URL, SecretEnv: e.SecretEnv, AllowLoopback: e.AllowLoopback})
		}
		prepared = append(prepared, client)
	}
	if err := (&storage.SQLite{DB: s.DB}).Configure(prepared); err != nil {
		return err
	}
	s.endpointsMu.Lock()
	s.Endpoints = runtime
	s.endpointsMu.Unlock()
	return nil
}
