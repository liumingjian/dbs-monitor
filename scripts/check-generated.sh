#!/bin/sh
set -eu

files='api/openapi.bundled.yaml
internal/api/server.gen.go
internal/api/client.gen.go
internal/alerting/db.go
internal/alerting/models.go
internal/alerting/queries.sql.go
internal/httpapi/db.go
internal/httpapi/models.go
internal/httpapi/queries.sql.go
internal/instance/db.go
internal/instance/models.go
internal/instance/queries.sql.go
internal/metric/db.go
internal/metric/models.go
internal/metric/queries.sql.go
web/src/api/schema.d.ts'

if git ls-files --error-unmatch api/openapi.bundled.yaml >/dev/null 2>&1; then
  make gen
  git diff --exit-code -- $files
  exit
fi

temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT
for file in $files; do
  mkdir -p "$temporary/$(dirname "$file")"
  cp "$file" "$temporary/$file"
done
make gen
for file in $files; do
  cmp "$temporary/$file" "$file"
done
