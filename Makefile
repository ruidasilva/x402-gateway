.PHONY: build test lint verify run clean demo docker-demo client deploy dashboard-dev dashboard-build

# ─── Build ──────────────────────────────────────────────────
# Only build tracked binaries. Do not reference local or gitignored paths.
build: dashboard-build
	go build -o bin/x402-server ./cmd/server
	go build -o bin/x402-client ./cmd/client

test:
	go test ./... -v -count=1

lint:
	go vet ./...

# ─── Verify ─────────────────────────────────────────────────
# Full pre-commit verification: build, lint, test, regression guard.
verify: lint test
	@echo "All checks passed."

run:
	go run ./cmd/server

clean:
	rm -rf bin/
	rm -rf cmd/server/static/

# ─── Dashboard ──────────────────────────────────────────────
dashboard-dev:
	cd dashboard && npm run dev

dashboard-build:
	cd dashboard && npm ci && npm run build

deploy:
	@if [ ! -f .env ]; then \
		echo "No .env found. Run 'make setup' or 'go run ./cmd/keygen' first."; \
		exit 1; \
	fi
	docker compose up -d --build

# ─── Docker Demo ─────────────────────────────────────────────
# Zero-config Docker startup: generates .env if missing, then builds.
docker-demo:
	@if [ ! -f .env ]; then \
		echo "No .env found — generating demo key..."; \
		go run ./cmd/keygen; \
		echo ""; \
	fi
	docker compose up --build

# ─── Demo mode ───────────────────────────────────────────────
# One command to go from zero to running:
#   make demo
#
# Generates a BSV key if needed, builds binaries, starts the
# server with auto-seeded pools, and opens the dashboard.

demo: build
	@if [ ! -f .env ]; then \
		echo "No .env found — generating demo key..."; \
		go run ./cmd/keygen; \
		echo ""; \
	fi
	@echo "  x402 Demo Mode"
	@echo "  ──────────────"
	@echo "  Dashboard:  http://localhost:8402/"
	@echo "  Client:     make client  (in another terminal)"
	@echo ""
	@set -a && . ./.env && set +a && ./bin/x402-server

# Run the CLI client against the local server
client:
	@go run ./cmd/client http://localhost:8402/v1/expensive
