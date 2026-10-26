.PHONY: run build fmt vet lint test check vuln up down integration live

COMPOSE ?= docker compose

run:
	go run ./cmd/app

build:
	go build -trimpath -o bin/secunda ./cmd/app

fmt:
	gofmt -w cmd config internal pkg migrations docs integration-test live-test

vet:
	go vet ./...

lint:
	golangci-lint run

test:
	go test -race -count=1 ./...

check: vet test build

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...

up:
	$(COMPOSE) up --build -d --wait

down:
	$(COMPOSE) down

integration:
	@trap '$(COMPOSE) -f compose.test.yaml down --volumes' EXIT; \
	$(COMPOSE) -f compose.test.yaml up --build --abort-on-container-exit --exit-code-from test

live:
	go test -race -tags live -count=1 -v ./live-test/...
