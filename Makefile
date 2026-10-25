BUILD_DIR := build
GOPRESS_BIN := $(BUILD_DIR)/gopress

# Where `go install` puts binaries: $GOBIN if set, else $GOPATH/bin, else ~/go/bin.
GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

# Packages whose concurrency contracts (plugin lifecycle, i18n snapshots,
# worker/scheduler shutdown, cache, hooks, engine runtime) carry race regressions.
RACE_PKGS := ./core ./core/plugin/... ./core/i18n/... ./core/worker/... ./core/cache/... ./core/hook/... ./core/api/...

# `make` with no target prints help.
.DEFAULT_GOAL := help

.PHONY: gopress server gen install uninstall clean help test test-race fmt-check lint

help:
	@echo "GoPress — Make targets"
	@echo ""
	@echo "  make gopress     Build the gopress CLI to $(GOPRESS_BIN)"
	@echo "  make server      Build the server binary (regenerates autoload first)"
	@echo "  make gen         Regenerate internal/autoload only"
	@echo "  make install     Install gopress to $(GOBIN)"
	@echo "  make uninstall   Remove gopress from $(GOBIN)"
	@echo "  make clean       Remove $(BUILD_DIR)/"
	@echo "  make test        Run all Go tests"
	@echo "  make test-race   Run concurrency regressions with the race detector"
	@echo "  make lint        Check gofmt and run go vet"
	@echo "  make help        Show this message"
	@echo ""
	@echo "Quickstart (no global install required):"
	@echo "  make gopress                                  # build CLI to $(GOPRESS_BIN)"
	@echo "  ./$(GOPRESS_BIN) serve                            # run server (auto-loads themes/plugins)"
	@echo "  ./$(GOPRESS_BIN) serve -config sites/foo.toml     # any flag passes through to cmd/server"
	@echo ""
	@echo "Optional: install globally so you can drop the ./$(BUILD_DIR)/ prefix:"
	@echo "  make install"
	@echo "  gopress serve"
	@echo ""
	@echo "Adding a new theme/plugin:"
	@echo "  Drop the folder into themes/ or plugins/, then re-run './$(GOPRESS_BIN) serve'."
	@echo "  A directory is auto-detected when it contains both:"
	@echo "    - theme.toml (themes) or plugin.toml (plugins)"
	@echo "    - at least one non-test .go file at its root"

# Catch unknown targets: print a hint, show help, exit non-zero.
.DEFAULT:
	@echo "make: unknown target '$@'"
	@echo ""
	@$(MAKE) --no-print-directory help
	@exit 2

gopress:
	@mkdir -p $(BUILD_DIR)
	@go build -o $(GOPRESS_BIN) ./cmd/gopress
	@echo "Built $(GOPRESS_BIN)"
	@echo "Run: ./$(GOPRESS_BIN) serve"

server: gopress
	@./$(GOPRESS_BIN) build

gen: gopress
	@./$(GOPRESS_BIN) gen

install:
	@go install ./cmd/gopress
	@echo "Installed $(GOBIN)/gopress"
	@case ":$$PATH:" in \
		*":$(GOBIN):"*) echo "PATH already contains $(GOBIN) — run: gopress serve" ;; \
		*) echo "NOTE: $(GOBIN) is not on PATH. Add it to your shell rc:"; \
		   echo "  export PATH=\"$(GOBIN):\$$PATH\"" ;; \
	esac

uninstall:
	@if [ -f "$(GOBIN)/gopress" ]; then \
		rm -f "$(GOBIN)/gopress"; \
		echo "Removed $(GOBIN)/gopress"; \
	else \
		echo "Nothing to remove at $(GOBIN)/gopress"; \
	fi

clean:
	@rm -rf $(BUILD_DIR)
	@echo "Removed $(BUILD_DIR)/"

test:
	@go test ./...

test-race:
	@go test -race $(RACE_PKGS)

# Only tracked files are checked so local, ignored workspaces do not fail CI.
fmt-check:
	@unformatted=$$(gofmt -l $$(git ls-files '*.go')); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; echo "$$unformatted"; exit 1; \
	fi

lint: fmt-check
	@go vet ./...
