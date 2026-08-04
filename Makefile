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

.PHONY: gen dev-up dev-down build check check-full package-binaries-linux-amd64 package-binaries-linux-arm64 package-linux-amd64 package-linux-arm64

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
	go build -tags embed_web -o dbs-monitor-server ./cmd/monitor-server
	go build -o dbs-monitor-agent ./cmd/monitor-agent

check:
	sh scripts/check-generated.sh
	go vet ./...
	go test ./...
	cd web && npm run typecheck
	cd web && npm run lint
	cd web && npm test -- --run

check-full: check
	$(MAKE) build
	sh scripts/check-e2e.sh
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./...

package-binaries-linux-amd64:
	cd web && npm ci && npm run build
	mkdir -p dist/bin/linux-amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags embed_web -trimpath -o dist/bin/linux-amd64/dbs-monitor-server ./cmd/monitor-server
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/bin/linux-amd64/dbs-monitor-agent ./cmd/monitor-agent

package-binaries-linux-arm64:
	cd web && npm ci && npm run build
	mkdir -p dist/bin/linux-arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags embed_web -trimpath -o dist/bin/linux-arm64/dbs-monitor-server ./cmd/monitor-server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o dist/bin/linux-arm64/dbs-monitor-agent ./cmd/monitor-agent

package-linux-amd64:
	TARGET_ARCH=amd64 sh scripts/package-linux.sh

package-linux-arm64:
	TARGET_ARCH=arm64 sh scripts/package-linux.sh
