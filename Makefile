.PHONY: all build test clean install help check-up-to-date lint

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

all: build

build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) .

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

test:
	go test ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || go vet ./...

clean:
	rm -f $(BUILD_DIR)/$(BINARY)

help:
	@echo "sb Makefile targets:"
	@echo "  make build   - Build the sb binary"
	@echo "  make install - Install sb to ~/.local/bin"
	@echo "  make test    - Run tests"
	@echo "  make lint    - Run linter (golangci-lint or go vet)"
	@echo "  make clean   - Remove build artifacts"
	@echo "  make help    - Show this help"
