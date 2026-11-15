SHELL := /bin/bash

VERSION ?= 0.1.0-dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X beat/internal/version.Version=$(VERSION) -X beat/internal/version.Commit=$(COMMIT) -X beat/internal/version.Date=$(DATE)

.PHONY: all deps web server agent build test dev demo clean

all: build

deps:
	cd web && npm install

web:
	cd web && npm run build

server:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/beat-server ./cmd/beat-server

agent:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/beat-agent ./cmd/beat-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/beat-agent-linux-amd64 ./cmd/beat-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/beat-agent-linux-arm64 ./cmd/beat-agent

build: web server agent

test:
	go test ./...
	go vet ./...
	cd web && npm run typecheck

dev:
	go run ./cmd/beat-server --demo --data-dir ./data

demo: dev

clean:
	rm -rf bin data web/dist

