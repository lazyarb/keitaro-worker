.PHONY: build test verify image

VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)" -o dist/keitaro-worker ./cmd/keitaro-worker

test:
	go test -race ./...

verify:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	go test -race ./...

image:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t lazyarb/keitaro-worker:local .
