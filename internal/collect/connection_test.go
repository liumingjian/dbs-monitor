package collect

import (
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/instance"
)

func TestTargetConnectionStringEscapesCredentials(t *testing.T) {
	connection := targetConnectionString(instance.ListCollectionTargetsRow{
		ID: pgtype.UUID{}, Host: "2001:db8::1", Port: 5432,
		DatabaseName: "db name", Username: "user name", Password: `space ' quote \\ slash`,
	})
	parsed, err := url.Parse(connection)
	if err != nil {
		t.Fatalf("parse connection URL: %v", err)
	}
	password, ok := parsed.User.Password()
	if !ok || parsed.User.Username() != "user name" || password != `space ' quote \\ slash` {
		t.Fatalf("credentials did not round-trip: %s", connection)
	}
	if parsed.Hostname() != "2001:db8::1" || parsed.Port() != "5432" || parsed.Path != "/db name" {
		t.Fatalf("target address did not round-trip: %s", connection)
	}
}
