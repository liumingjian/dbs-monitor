package httpapi

import (
	"errors"
	"fmt"
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
		name        string
		err         error
		wantCode    api.ErrorErrorCode
		wantMessage string
	}{
		{
			name:        "authentication",
			err:         fmt.Errorf("connect: %w", &pgxconn.PgError{Code: "28P01"}),
			wantCode:    api.AUTHFAILED,
			wantMessage: "目标 PostgreSQL 认证失败",
		},
		{
			name:        "network",
			err:         errors.New("connection refused"),
			wantCode:    api.NETWORKUNREACHABLE,
			wantMessage: "无法连接目标 PostgreSQL",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := classifyTargetConnectionError(test.err)
			if failure.code != test.wantCode || failure.message != test.wantMessage {
				t.Fatalf("classification = %#v, want code %s and message %q", failure, test.wantCode, test.wantMessage)
			}
		})
	}
}
