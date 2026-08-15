#!/bin/sh
set -eu

install -o postgres -g postgres -m 0644 /acceptance-tls/server.crt /var/lib/postgresql/server.crt
install -o postgres -g postgres -m 0600 /acceptance-tls/server.key /var/lib/postgresql/server.key

exec /usr/local/bin/docker-entrypoint.sh "$@"
