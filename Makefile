.PHONY: up down logs migrate seed fmt test vet check

up:
	docker compose up -d --build --wait app
down:
	docker compose down
logs:
	docker compose logs -f app migrate db
migrate:
	docker compose run --rm --build migrate up
seed:
	docker compose exec -T db sh -c 'psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"' < db/seeds/development.sql
fmt:
	gofmt -w cmd internal pkg db/migrations
test:
	go test ./...
vet:
	go vet ./...
check: test vet
	git diff --check
