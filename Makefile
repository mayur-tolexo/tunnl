BIN_DIR := bin
CLIENT  := $(BIN_DIR)/tunnl
RELAY   := $(BIN_DIR)/tunnld
PORT    ?= 3000

# Local dev defaults (plain HTTP, no TLS/GoDaddy)
DEV_DOMAIN ?= localhost
DEV_PORT   ?= 8088
DEV_TOKEN  ?= devtoken

.DEFAULT_GOAL := build

.PHONY: build build-client build-relay install run-client run-relay dev-relay dev-client test race vet fmt clean help

## build: compile both binaries into ./bin
build: build-client build-relay

build-client:
	go build -o $(CLIENT) ./cmd/tunnl

build-relay:
	go build -o $(RELAY) ./cmd/tunnld

## install: install tunnl and tunnld into $GOBIN (usually ~/go/bin, must be on PATH)
install:
	go install ./cmd/tunnl ./cmd/tunnld

## run-client: build then run the client, e.g. make run-client PORT=3000
##             requires TUNNL_RELAY and TUNNL_TOKEN in the environment
run-client: build-client
	$(CLIENT) http $(PORT)

## run-relay: build then run the relay (binds :80 and :443, needs sudo)
##            requires TUNNL_TOKEN, TUNNL_DOMAIN, TUNNL_ACME_EMAIL, TUNNL_GODADDY_KEY/SECRET
run-relay: build-relay
	sudo -E $(RELAY)

## dev-relay: run the relay locally over plain HTTP (no TLS/GoDaddy) on :$(DEV_PORT)
dev-relay: build-relay
	TUNNL_HTTP_ADDR=:$(DEV_PORT) TUNNL_DOMAIN=$(DEV_DOMAIN) TUNNL_TOKEN=$(DEV_TOKEN) $(RELAY)

## dev-client: run the client against the local dev relay, e.g. make dev-client PORT=3000
dev-client: build-client
	TUNNL_RELAY=ws://tunnl.$(DEV_DOMAIN):$(DEV_PORT)/tunnel TUNNL_TOKEN=$(DEV_TOKEN) $(CLIENT) http $(PORT)

## test: run the test suite
test:
	go test ./...

## race: run the test suite with the race detector
race:
	go test -race -count=1 ./...

## vet: run go vet
vet:
	go vet ./...

## fmt: format all Go files
fmt:
	gofmt -w .

## clean: remove build output
clean:
	rm -rf $(BIN_DIR)

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
