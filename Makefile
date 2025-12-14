.PHONY: all test lint scan build clean

all: test lint scan build

test:
	go test -v -race -cover ./...

lint:
	golangci-lint run

scan:
	./scripts/security_scan.sh

build:
	go build -o bin/auditd ./cmd/auditd
	go build -o bin/api ./cmd/api
	go build -o bin/vault ./cmd/vault

clean:
	rm -rf bin
