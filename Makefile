SHELL := /bin/bash
export API_TOKEN ?= local-dev-token
export BASE_URL ?= http://localhost:8080
export KAFKA_BROKERS ?= localhost:19092

.PHONY: help up down logs test test-unit test-api lint fmt load profile

help: ## Show targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-10s %s\n", $$1, $$2}'

up: ## Build and start the stack, wait until healthy
	docker compose up -d --build --wait

down: ## Stop the stack and drop its data
	docker compose down -v

logs: ## Follow service logs
	docker compose logs -f broker

test: test-unit test-api ## Unit + API tests

test-unit: ## Service unit tests and framework self-tests
	cd service && go test -race ./...
	cd tests && go test -race ./internal/...

test-api: ## Black-box API tests against the running stack
	cd tests && go test -race -count=1 ./api/...

lint: ## gofmt + go vet for both modules
	@test -z "$$(gofmt -l service tests)" || (echo "gofmt needed:"; gofmt -l service tests; exit 1)
	cd service && go vet ./...
	cd tests && go vet ./...

fmt: ## Format code
	gofmt -w service tests

load: ## k6 load test (needs the stack up)
	docker run --rm -i --network host -e BASE_URL -e API_TOKEN grafana/k6:0.54.0 run - < load/orders.js

profile: ## 30s CPU profile of the running service (pprof on :6060)
	go tool pprof -http=:7070 "http://127.0.0.1:6060/debug/pprof/profile?seconds=30"
