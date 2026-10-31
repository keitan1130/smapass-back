.PHONY: up dev down logs migrate seed smoke fmt fmt-check generate generate-check test test-race test-integration vet build check

GO ?= go
DEV_COMPOSE = docker compose -f compose.yaml -f compose.dev.yaml

up:
	docker compose up -d --build --wait app
dev:
	$(DEV_COMPOSE) up -d --build --wait app
down:
	docker compose down
logs:
	docker compose logs -f app migrate db
migrate:
	docker compose run --rm --build migrate up
seed:
	docker compose exec -T db sh -c 'psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"' < db/seeds/development.sql
smoke:
	python3 scripts/smoke.py
fmt:
	gofmt -w cmd internal pkg db tools
fmt-check:
	@files=$$(gofmt -l cmd internal pkg db tools); if [ -n "$$files" ]; then echo "Run make fmt:"; echo "$$files"; exit 1; fi
generate:
	$(GO) generate ./internal/domain/repository
	$(GO) run ./tools/openapi
generate-check:
	$(GO) run ./tools/openapi -check
	sh scripts/check-mocks.sh
test:
	$(GO) test ./...
test-race:
	$(GO) test -race -coverpkg=./... -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out
test-integration:
	$(GO) test -count=1 -tags=integration -timeout=5m ./internal/infrastructure/...
vet:
	$(GO) vet ./...
build:
	$(GO) build -o bin/server ./cmd
	$(GO) build -o bin/migrate ./cmd/migrate
check: fmt-check generate-check vet test build
	$(GO) mod verify
	git diff --check
