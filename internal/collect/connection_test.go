package collect

import (
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/instance"
)

func TestTargetConnectionConfigSetsPasswordDirectly(t *testing.T) {
	target := instance.ListCollectionTargetsRow{
		Host: "2001:db8::1", Port: 5432,
		DatabaseName: instance.BootstrapDatabaseColumn("db name"), Username: "user name",
	}
	config, err := targetConnectionConfig(target, `space ' quote \\ slash`)
	if err != nil {
		t.Fatalf("build connection config: %v", err)
	}
	if config.User != "user name" || config.Password != `space ' quote \\ slash` {
		t.Fatalf("credentials did not round-trip in config: user=%q", config.User)
	}
	if config.Host != "2001:db8::1" || config.Port != 5432 || config.Database != "db name" {
		t.Fatalf("target address did not round-trip: host=%q port=%d database=%q", config.Host, config.Port, config.Database)
	}
}
