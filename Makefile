.PHONY: all build test clean lint install help check-up-to-date version-info build-all quickstart

GO ?= go
BINARY := sb
BUILD_DIR := .
INSTALL_DIR := $(HOME)/.local/bin

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -ldflags "-s -w \
	-X main.Version=$(VERSION) \
	-X main.GitCommit=$(COMMIT) \
	-X main.BuildDate=$(BUILD_DATE)"

GO_FILES := $(shell find . -name '*.go' -type f)

PLATFORMS := linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 freebsd-amd64

all: build

# Build
build: $(BINARY)

$(BINARY): $(GO_FILES)
	$(GO) build $(LDFLAGS) -o $@ .

# Cross-compilation
define build-platform
$(BINARY)-$(1): $(GO_FILES)
	GOOS=$(word 1,$(subst -, ,$(1))) GOARCH=$(word 2,$(subst -, ,$(1))) \
		$(GO) build $(LDFLAGS) -o $$@ .
endef

$(foreach p,$(PLATFORMS),$(eval $(call build-platform,$(p))))

build-all: $(addprefix $(BINARY)-,$(PLATFORMS))

# Install
check-up-to-date:
ifndef SKIP_UPDATE_CHECK
	@git fetch origin main --quiet 2>/dev/null || true
	@LOCAL=$$(git rev-parse HEAD 2>/dev/null); \
	REMOTE=$$(git rev-parse origin/main 2>/dev/null); \
	if [ -n "$$REMOTE" ] && [ "$$LOCAL" != "$$REMOTE" ]; then \
		echo "ERROR: Local branch is not up to date with origin/main"; \
		echo "Run 'git pull' first, or use SKIP_UPDATE_CHECK=1 to override"; \
		exit 1; \
	fi
endif

install: check-up-to-date build
	@mkdir -p $(INSTALL_DIR)
	@rm -f $(INSTALL_DIR)/$(BINARY)
	@cp $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	@for bad in $(HOME)/go/bin/$(BINARY) $(HOME)/bin/$(BINARY); do \
		if [ -f "$$bad" ]; then \
			echo "Removing stale $$bad (use make install, not go install)"; \
			rm -f "$$bad"; \
		fi; \
	done
	@echo "Installed $(BINARY) to $(INSTALL_DIR)/$(BINARY)"

# Test
test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

# Lint
lint:
	@echo "==> Go: vet"
	$(GO) vet ./...
	@echo "==> Go: fmt check"
	@gofmt -l $(GO_FILES) | tee /dev/stderr | (! read)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "==> Go: golangci-lint"; \
		golangci-lint run; \
	fi

# Format
fmt:
	gofmt -w $(GO_FILES)

# Version
version-info:
	@echo "VERSION:    $(VERSION)"
	@echo "COMMIT:     $(COMMIT)"
	@echo "BUILD_DATE: $(BUILD_DATE)"

# Quickstart (agent-facing setup context)
quickstart: build
	./$(BINARY) quickstart

# Clean
clean:
	rm -f $(BINARY) $(BINARY)-*

# Help
help:
	@echo "sb Makefile targets:"
	@echo ""
	@echo "Build:"
	@echo "  make build       Build the sb binary"
	@echo "  make build-all   Cross-compile for all platforms"
	@echo "  make install     Build and install to ~/.local/bin"
	@echo ""
	@echo "Test:"
	@echo "  make test        Run tests"
	@echo "  make test-race   Run tests with race detector"
	@echo ""
	@echo "Lint:"
	@echo "  make lint        Run go vet, fmt check, golangci-lint"
	@echo "  make fmt         Auto-format Go files"
	@echo ""
	@echo "Other:"
	@echo "  make clean        Remove build artifacts"
	@echo "  make version-info Show embedded version info"
	@echo "  make help         Show this help"
	@echo ""
	@echo "Variables:"
	@echo "  GO=go124                  Use alternate go binary"
	@echo "  SKIP_UPDATE_CHECK=1       Skip origin/main freshness check"
	@echo ""
	@echo "FreeBSD: use gmake instead of make"
