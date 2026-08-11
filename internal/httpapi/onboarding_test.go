package httpapi

import (
	"errors"
	"testing"

	pgxconn "github.com/jackc/pgx/v5/pgconn"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestTargetVersionSupported(t *testing.T) {
	tests := []struct {
		name             string
		serverVersionNum int
		want             bool
	}{
		{name: "below minimum", serverVersionNum: 120000, want: false},
		{name: "minimum version", serverVersionNum: 130000, want: true},
		{name: "newer PostgreSQL", serverVersionNum: 180000, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := targetVersionSupported(test.serverVersionNum); got != test.want {
				t.Fatalf("targetVersionSupported(%d) = %t, want %t", test.serverVersionNum, got, test.want)
			}
		})
	}
}

func TestClassifyTargetConnectionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want api.ErrorErrorCode
	}{
		{name: "authentication", err: &pgxconn.PgError{Code: "28P01"}, want: api.AUTHFAILED},
		{name: "network", err: errors.New("connection refused"), want: api.NETWORKUNREACHABLE},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := classifyTargetConnectionError(test.err)
			if failure.code != test.want {
				t.Fatalf("classification = %#v, want %s", failure, test.want)
			}
		})
	}
}
