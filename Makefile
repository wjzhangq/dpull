.PHONY: build clean test install release help

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/wjzhangq/dpull/pkg/version.Version=$(VERSION) \
	-X github.com/wjzhangq/dpull/pkg/version.Commit=$(COMMIT) \
	-X github.com/wjzhangq/dpull/pkg/version.BuildDate=$(BUILD_DATE)

BINDIR ?= bin
BINARY := $(BINDIR)/dpull

help:
	@echo "dpull Makefile targets:"
	@echo "  build            Build dpull binary"
	@echo "  clean            Remove build artifacts"
	@echo "  test             Run all tests"
	@echo "  test-verbose     Run tests with verbose output"
	@echo "  install          Install dpull to GOPATH/bin"
	@echo "  release          Build release binaries for all platforms"
	@echo "  lint             Run golangci-lint (requires golangci-lint)"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION=$(VERSION)"
	@echo "  COMMIT=$(COMMIT)"
	@echo "  BUILD_DATE=$(BUILD_DATE)"

build:
	@mkdir -p $(BINDIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/dpull
	@echo "Built: $(BINARY)"
	@$(BINARY) --version

clean:
	rm -rf $(BINDIR) dist

test:
	go test ./...

test-verbose:
	go test -v ./...

install:
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/dpull

lint:
	golangci-lint run ./...

# Cross-compile for multiple platforms
release:
	@mkdir -p dist
	@for platform in \
		linux/amd64 \
		linux/arm64 \
		darwin/amd64 \
		darwin/arm64 \
		windows/amd64; do \
		goos=$${platform%/*}; \
		goarch=$${platform#*/}; \
		output="dist/dpull_$(VERSION)_$${goos}_$${goarch}"; \
		[ "$$goos" = "windows" ] && output="$${output}.exe" || true; \
		echo "Building $$goos/$$goarch..."; \
		GOOS=$$goos GOARCH=$$goarch CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o "$$output" ./cmd/dpull || exit 1; \
	done
	@echo "Release binaries created in dist/"
	@ls -lh dist/
