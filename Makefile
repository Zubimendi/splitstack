.PHONY: up down migrate seed run test test-integration

up:
	docker-compose up -d

down:
	docker-compose down -v

migrate:
	docker exec -i $$(docker-compose ps -q db) psql -U splitstack -d splitstack < internal/db/migrations/000001_init.up.sql
	docker exec -i $$(docker-compose ps -q db) psql -U splitstack -d splitstack < internal/db/migrations/000002_add_passwords.up.sql

seed:
	go run cmd/seed/main.go

run:
	go run cmd/api/main.go

test:
	go test ./internal/ledger/... -run 'Split|Settlement' -v

test-integration: up migrate
	go test ./test/integration/... -v
