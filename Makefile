# BatoX - build and deploy helpers. Run `make help` for the list of targets.

APP       := batox
MAIN      := ./cmd/batox
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)
BUILD_DIR := build

.PHONY: help build build-arm64 build-amd64 build-host run-dev test vet fmt fmt-check tidy deploy clean

help: ## show this help
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

build: build-arm64 ## build for Raspberry Pi (alias of build-arm64)

build-arm64: ## build for Batocera on Raspberry Pi 4/5 (linux/arm64)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP)-linux-arm64 $(MAIN)

build-amd64: ## build for Batocera on x86_64 PCs (linux/amd64)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP)-linux-amd64 $(MAIN)

build-host: ## build for the current machine
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP) $(MAIN)

run-dev: ## run locally with config.dev.json (files under ./dev)
	go run $(MAIN) -config config.dev.json

test: ## run the unit tests
	go test ./...

vet: ## run go vet
	go vet ./...

fmt: ## format the Go code
	gofmt -w ./cmd ./internal

fmt-check: ## fail if any Go file is not formatted
	@test -z "$$(gofmt -l ./cmd ./internal)" || (gofmt -l ./cmd ./internal; echo "run: make fmt"; exit 1)

tidy: ## tidy go.mod and go.sum
	go mod tidy

deploy: ## build and install on Batocera (BATOCERA_HOST=ip, BATOX_ARCH=amd64 for PCs)
	$(MAKE) build-$${BATOX_ARCH:-arm64}
	./scripts/deploy.sh deploy

clean: ## remove build output
	rm -rf $(BUILD_DIR)
