# serial-proxy Makefile — all Go commands run inside Podman containers.
# NO local Go toolchain required.
#
# Usage:
#   make build       — compile static binary
#   make test        — run all tests with race detector
#   make lint        — run golangci-lint v2
#   make all         — build + test + lint
#   make deploy      — build + scp to pve-netlab + restart service
#   make clean       — remove build artifacts

GO_IMAGE := docker.io/library/golang:1.26
LINT_IMAGE := docker.io/golangci/golangci-lint:v2.1.6
BINARY := serial-proxy
PVE_HOST := pve-netlab
PVE_DEST := /usr/local/bin/$(BINARY)

PODMAN_RUN := podman run --rm -v $(CURDIR):/src:Z -w /src
GO := $(PODMAN_RUN) -e CGO_ENABLED=0 $(GO_IMAGE)
LINT := $(PODMAN_RUN) $(LINT_IMAGE)

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test lint clean deploy fmt tidy vulncheck

all: build test lint

# === Build ===

build:
	$(GO) go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/serial-proxy/

# === Test ===

test:
	$(GO) go test -race -count=1 -timeout=60s ./...

# === Lint ===

lint:
	$(LINT) golangci-lint run ./...

# === Format ===

fmt:
	$(GO) gofumpt -w .

tidy:
	$(GO) sh -c 'go mod tidy && go mod verify'

# === Security ===

vulncheck:
	$(GO) sh -c 'go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...'

# === Deploy ===

deploy: build
	tsh ssh root@$(PVE_HOST) 'systemctl stop $(BINARY) 2>/dev/null; true'
	tsh scp $(BINARY) root@$(PVE_HOST):$(PVE_DEST)
	tsh scp deployments/systemd/$(BINARY).service root@$(PVE_HOST):/etc/systemd/system/$(BINARY).service
	tsh ssh root@$(PVE_HOST) 'chmod +x $(PVE_DEST) && systemctl daemon-reload && systemctl enable --now $(BINARY)'
	@echo "Deployed $(VERSION) to $(PVE_HOST)"

# === Clean ===

clean:
	rm -f $(BINARY)
	rm -rf dist/
