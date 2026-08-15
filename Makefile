SHELL := /bin/sh

PGHOST ?= localhost
PGPORT ?= 55432
PGUSER ?= dbs_monitor
PGDATABASE ?= dbs_monitor
PGPASSWORD ?= dbs_monitor
export PGHOST PGPORT PGUSER PGDATABASE PGPASSWORD
DATABASE_URL ?= postgres://$(PGUSER):$(PGPASSWORD)@$(PGHOST):$(PGPORT)/$(PGDATABASE)?sslmode=disable
export DATABASE_URL

OAPI_CODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.0
REDOCLY := npx --yes @redocly/cli@2.20.3
OPENAPI_TYPESCRIPT := npx --yes openapi-typescript@7.13.0
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0
GOOSE := go run github.com/pressly/goose/v3/cmd/goose@v3.24.3

CANDIDATE_SHA := $(shell git rev-parse HEAD)
CANDIDATE_TAG := $(shell git describe --exact-match HEAD 2>/dev/null)
BUILD_VERSION := $(if $(CANDIDATE_TAG),$(patsubst v%,%,$(CANDIDATE_TAG)),0.0.0-dev+$(CANDIDATE_SHA))
BUILD_LDFLAGS := -X main.version=$(BUILD_VERSION) -X main.commitSHA=$(CANDIDATE_SHA)

.PHONY: gen dev-up dev-down build check check-full check-pg-matrix check-snapshot-matrix check-sqlc-vet

gen:
	$(REDOCLY) bundle api/openapi.yaml --output api/openapi.bundled.yaml
	$(OAPI_CODEGEN) --config api/oapi-server.yaml api/openapi.bundled.yaml
	$(OAPI_CODEGEN) --config api/oapi-client.yaml api/openapi.bundled.yaml
	$(OPENAPI_TYPESCRIPT) api/openapi.bundled.yaml -o web/src/api/schema.d.ts
	$(SQLC) generate

dev-up:
	@if [ -n "$${PGHOST_EXTERNAL:-}" ]; then \
		echo "using external PostgreSQL at $${PGHOST}:$${PGPORT}"; \
	else \
		docker compose up -d --wait postgres; \
	fi

dev-down:
	docker compose down

build:
	cd web && npm run build
	go build -ldflags "$(BUILD_LDFLAGS)" -tags embed_web -o dbs-monitor-server ./cmd/monitor-server
	go build -ldflags "$(BUILD_LDFLAGS)" -o dbs-monitor-agent ./cmd/monitor-agent

check:
	sh scripts/check-toolchain.sh
	sh scripts/check-generated.sh
	go vet ./...
	go test -p 1 ./...
	cd web && npm run typecheck
	cd web && npm run lint
	cd web && npm test -- --run

check-full: check
	$(MAKE) build
	sh scripts/check-e2e.sh
	$(MAKE) check-sqlc-vet
	$(MAKE) check-pg-matrix

check-sqlc-vet:
	@database=dbs_monitor_sqlc_vet_$$$$; \
	cleanup() { \
		PGPASSWORD="$(PGPASSWORD)" psql -h "$(PGHOST)" -p "$(PGPORT)" -U "$(PGUSER)" -d postgres \
			-c "DROP DATABASE IF EXISTS \"$$database\" WITH (FORCE)" >/dev/null 2>&1 || true; \
	}; \
	trap cleanup EXIT INT TERM; \
	PGPASSWORD="$(PGPASSWORD)" psql -h "$(PGHOST)" -p "$(PGPORT)" -U "$(PGUSER)" -d postgres -v ON_ERROR_STOP=1 \
		-c "CREATE DATABASE \"$$database\" TEMPLATE template0 LC_COLLATE 'C' LC_CTYPE 'C'" >/dev/null; \
	vet_url="postgres://$(PGUSER):$(PGPASSWORD)@$(PGHOST):$(PGPORT)/$$database?sslmode=disable"; \
	$(GOOSE) -dir migrations postgres "$$vet_url" up; \
	$(GOOSE) -dir migrations postgres "$$vet_url" up; \
	DATABASE_URL="$$vet_url" $(SQLC) vet

check-pg-matrix:
	docker compose --profile matrix up -d --wait monitored-pg13 monitored-pg14 monitored-pg15 monitored-pg16 monitored-pg17 monitored-pg17-replica
	PG13_URL=postgres://monitored:monitored@localhost:55433/monitored?sslmode=disable \
	PG14_URL=postgres://monitored:monitored@localhost:55434/monitored?sslmode=disable \
	PG15_URL=postgres://monitored:monitored@localhost:55435/monitored?sslmode=disable \
	PG16_URL=postgres://monitored:monitored@localhost:55436/monitored?sslmode=disable \
	PG17_URL=postgres://monitored:monitored@localhost:55437/monitored?sslmode=disable \
	PG17_REPLICA_URL=postgres://monitored:monitored@localhost:55438/monitored?sslmode=disable \
	go test ./internal/metric -run 'TestPG((StatDatabase|StatActivity|StatStatements|Replication|ReplicationSlot|PreparedXacts|Role)ShapeMatrix|ReplicationStandbyView)' -count=1

check-snapshot-matrix:
	docker compose --profile matrix up -d --wait
	SNAPSHOT_MATRIX_PORTS="55433 55434 55435 55436 55437" go test ./internal/evaluator -run TestTriggerSnapshotQueryPGMatrix -count=1
