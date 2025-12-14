.PHONY: all test test-ledger lint scan build clean

all: test lint scan build

test:
    go test -v -race -cover ./...

test-ledger:
    @echo "Running ledger tests with PostgreSQL..."
    @docker-compose -f deploy/docker-compose.test.yml up -d postgres
    @sleep 5
    @DATABASE_URL=postgres://ledger:password@localhost:5432/ledger_test go test -v -race ./internal/ledger/...
    @docker-compose -f deploy/docker-compose.test.yml down

lint:
    golangci-lint run

scan:
    ./scripts/security_scan.sh

build:
    go build -o bin/auditd ./cmd/auditd
    go build -o bin/api ./cmd/api
    go build -o bin/vault ./cmd/vault
    go build -o bin/ledger ./cmd/ledger

clean:
    rm -rf bin
