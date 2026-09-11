.DEFAULT_GOAL := help
SHELL := /bin/bash

# Go and the Android/Flutter toolchain live under $HOME (installed without
# sudo), so every target sets its own PATH rather than relying on the shell.
export PATH := /home/rock/.local/go/bin:/home/rock/.local/flutter/bin:/home/rock/Android/Sdk/platform-tools:$(PATH)
export GOPATH := /home/rock/.local/gopath
export JAVA_HOME := /home/rock/.local/jdk17
export ANDROID_HOME := /home/rock/Android/Sdk

VERSION := $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

TEST_DATABASE_URL ?= postgres://braelaspin:devpassword@localhost:5433/braelaspin_test?sslmode=disable

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /' | column -t -s ':'

## up: start postgres + redis
up:
	docker compose -f deploy/docker-compose.yml up -d
	@echo "waiting for health..."
	@for i in $$(seq 1 30); do \
	  if docker compose -f deploy/docker-compose.yml ps --format json 2>/dev/null | grep -q '"Health":"healthy"'; then break; fi; \
	  sleep 1; \
	done
	@docker compose -f deploy/docker-compose.yml ps

## down: stop the stack (keeps data)
down:
	docker compose -f deploy/docker-compose.yml down

## nuke: stop the stack and DELETE the database volume
nuke:
	docker compose -f deploy/docker-compose.yml down -v

## migrate: apply pending migrations
migrate:
	set -a && source .env && set +a && go run ./cmd/api migrate up

## migrate-status: show applied/pending migrations
migrate-status:
	set -a && source .env && set +a && go run ./cmd/api migrate status

## run: run the API server against .env
run:
	set -a && source .env && set +a && go run ./cmd/api serve

## build: build a stripped static binary into bin/
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/braelaspin ./cmd/api
	@ls -lh bin/braelaspin

## test: unit tests (no database required)
test:
	go test ./... -count=1

## test-race: unit tests under the race detector
test-race:
	go test ./... -count=1 -race

## test-integration: tests that need a real postgres
test-integration:
	@docker compose -f deploy/docker-compose.yml exec -T db \
	  psql -U braelaspin -d braelaspin -c "SELECT 1" >/dev/null 2>&1 || { echo "run 'make up' first"; exit 1; }
	@docker compose -f deploy/docker-compose.yml exec -T db \
	  psql -U braelaspin -d postgres -c "DROP DATABASE IF EXISTS braelaspin_test" >/dev/null
	@docker compose -f deploy/docker-compose.yml exec -T db \
	  psql -U braelaspin -d postgres -c "CREATE DATABASE braelaspin_test" >/dev/null
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./... -count=1 -tags=integration

## fuzz: fuzz the parsers that face untrusted input
fuzz:
	go test ./internal/auth -run=XXX -fuzz=FuzzNormalise -fuzztime=60s

## vet: go vet
vet:
	go vet ./...

## tunnel: expose localhost:8080 so Daraja callbacks can reach this machine
tunnel:
	@command -v cloudflared >/dev/null || { echo "install cloudflared first"; exit 1; }
	@echo "Paste these into the Daraja portal once the tunnel URL appears:"
	@echo "  STK callback : <url>/webhooks/mpesa/stk/\$$MPESA_CALLBACK_SECRET"
	@echo "  B2C result   : <url>/webhooks/mpesa/b2c/result/\$$MPESA_CALLBACK_SECRET"
	@echo "  B2C timeout  : <url>/webhooks/mpesa/b2c/timeout/\$$MPESA_CALLBACK_SECRET"
	cloudflared tunnel --url http://localhost:8080

## app-run: run the Flutter app on a connected device
app-run:
	cd app && flutter run --dart-define=API_BASE=http://10.0.2.2:8080

## app-apk: build split release APKs
app-apk:
	cd app && flutter build apk --release --split-per-abi \
	  --target-platform android-arm,android-arm64 \
	  --obfuscate --split-debug-info=build/symbols --tree-shake-icons
	@ls -lh app/build/app/outputs/flutter-apk/*.apk

## app-size: report APK size against the budget
app-size:
	@for f in app/build/app/outputs/flutter-apk/*-release.apk; do \
	  [ -f "$$f" ] || continue; \
	  sz=$$(stat -c %s "$$f"); \
	  printf "%-50s %6.1f MB\n" "$$(basename $$f)" "$$(echo "scale=1; $$sz/1048576" | bc)"; \
	done

.PHONY: help up down nuke migrate migrate-status run build test test-race test-integration fuzz vet tunnel app-run app-apk app-size
