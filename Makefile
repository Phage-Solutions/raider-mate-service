.PHONY: run test test-integration lint migrate sqlc up down fmt

# Local config lives in .env. Nothing in the service reads it, so make exports it
# for the targets that need it. Real environments set the variables directly.
ifneq (,$(wildcard .env))
include .env
export
endif

run:
	go run ./cmd/api

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

lint:
	go tool golangci-lint run

migrate:
	@test -n "$(DATABASE_URL)" || { echo "DATABASE_URL is not set; cp .env.example .env"; exit 1; }
	go tool goose -dir migrations postgres "$(DATABASE_URL)" up

sqlc:
	go tool sqlc generate

up:
	docker compose up -d

down:
	docker compose down

fmt:
	gofmt -l -w .
