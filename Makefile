.PHONY: test build smoke failures backup-test ci tenants capacity benchmark web-install web-build web-test playground playground-test docker-build coverage lint vulncheck storage-full-test storage-io-test
test:
	go test -race ./...
build:
	mkdir -p bin
	go build -o bin/dispatcher ./cmd/dispatcher
	go build -o bin/receiver ./cmd/receiver
	go build -o bin/playground ./cmd/playground

smoke: build
	python3 scripts/smoke.py

failures: build
	python3 scripts/failures.py

backup-test: build
	python3 -B scripts/test_backup.py

# Same sequence locally and in GitHub Actions; process tests run sequentially.
ci:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }
	go mod verify
	$(MAKE) test
	go vet ./...
	$(MAKE) build
	python3 -B scripts/test_smoke.py
	python3 -B scripts/smoke.py
	python3 -B scripts/failures.py
	python3 -B scripts/test_backup.py
	python3 -B scripts/tenants.py
	python3 -B scripts/capacity.py
	$(MAKE) web-install web-test web-build
	python3 -B scripts/playground_smoke.py

tenants: build
	python3 -B scripts/tenants.py

capacity: build
	python3 -B scripts/capacity.py

# BENCH_ARGS must include a new --output path; benchmark is not part of CI.
benchmark: build
	python3 -B scripts/benchmark.py $(BENCH_ARGS)

web-install:
	npm ci --cache "$${NPM_CONFIG_CACHE:-/tmp/hookrelay-npm}" --prefix web/playground

web-build:
	npm --prefix web/playground run build

web-test:
	npm --prefix web/playground test

playground: build web-build
	./bin/playground

playground-test: build web-build
	python3 -B scripts/playground_smoke.py

docker-build:
	docker build -t hookrelay:latest .

coverage:
	go test -race -covermode=atomic -coverprofile=coverage.out -coverpkg=./... ./internal/...
	@go tool cover -func=coverage.out | tail -n 1
	@rm -f coverage.out

# Standard Go checks are identical on every machine; no optional linter fallback.
lint:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }
	go vet ./...

# Versioned tool; download or vulnerability database failures fail this target.
GOVULNCHECK_VERSION := v1.9.0
vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# Actual ENOSPC runs only in a bounded, disposable Docker tmpfs.
storage-full-test:
	python3 -B scripts/storage_full.py

# WAL EIO injection and interrupted uncommitted frames, isolated from the host.
storage-io-test:
	python3 -B scripts/storage_full.py --suite io
