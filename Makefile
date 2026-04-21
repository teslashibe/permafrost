.PHONY: help build test lint fmt vet tidy clean up down logs migrate sqlc tools demo demo-clean demo-bittensor demo-bittensor-clean

GO ?= go
BIN_DIR := bin
BINS := permafrost permafrostd
LDFLAGS := -s -w

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build all binaries into ./bin
	@mkdir -p $(BIN_DIR)
	@for b in $(BINS); do \
		echo "==> building $$b"; \
		$(GO) build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$$b ./cmd/$$b; \
	done

test: ## Run unit tests
	$(GO) test ./... -race -count=1

lint: ## Run go vet + golangci-lint (if installed)
	$(GO) vet ./...
	@which golangci-lint > /dev/null && golangci-lint run ./... || echo "golangci-lint not installed; skipping"

fmt: ## Format code
	$(GO) fmt ./...

vet: ## Run go vet
	$(GO) vet ./...

tidy: ## Tidy go.mod
	$(GO) mod tidy

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

up: ## Bring up local stack (Timescale + permafrostd)
	docker compose -f deploy/compose/docker-compose.yml up -d

down: ## Tear down local stack
	docker compose -f deploy/compose/docker-compose.yml down

logs: ## Tail logs for the local stack
	docker compose -f deploy/compose/docker-compose.yml logs -f

migrate: ## Apply database migrations
	$(GO) run ./cmd/permafrost db migrate up

sqlc: ## Regenerate sqlc bindings
	@which sqlc > /dev/null || (echo "install sqlc: https://docs.sqlc.dev/en/latest/overview/install.html" && exit 1)
	sqlc generate

tools: ## Install dev tools
	$(GO) install github.com/pressly/goose/v3/cmd/goose@latest
	$(GO) install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

demo: ## One-command demo: build + stack + init + recruit Pip + tail decisions
	@./scripts/demo.sh

demo-clean: ## Tear down the demo: stop stack + remove the demo config dir
	@echo "==> stopping stack"
	@docker compose -f deploy/compose/docker-compose.yml down -v 2>/dev/null || true
	@echo "==> removing .permafrost-demo/"
	@rm -rf .permafrost-demo
	@echo "demo cleaned. Run \`make demo\` to start fresh."

demo-bittensor: ## Bittensor demo: stack + subtensor + 3 alpha agents (Tao, Mo, Yumi)
	@./scripts/demo-bittensor.sh

demo-bittensor-clean: ## Tear down the bittensor demo (incl. subtensor container)
	@echo "==> stopping noise trader (if running)"
	@if [ -f .permafrost-demo-bittensor/noise-trader.pid ]; then \
		kill `cat .permafrost-demo-bittensor/noise-trader.pid` 2>/dev/null || true; \
	fi
	@echo "==> stopping stack + subtensor"
	@docker compose -f deploy/compose/docker-compose.yml --profile bittensor down -v 2>/dev/null || true
	@echo "==> removing .permafrost-demo-bittensor/"
	@rm -rf .permafrost-demo-bittensor
	@echo "==> clearing apps/desk/.env.local"
	@rm -f apps/desk/.env.local
	@echo "bittensor demo cleaned. Run \`make demo-bittensor\` to start fresh."
