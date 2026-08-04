package pgconn

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type TargetConn struct {
	*pgx.Conn
}

type Dialer interface {
	Dial(context.Context, string) (*TargetConn, error)
}

type DirectDialer struct{}

func (DirectDialer) Dial(ctx context.Context, connectionString string) (*TargetConn, error) {
	conn, err := pgx.Connect(ctx, connectionString)
	if err != nil {
		return nil, err
	}
	return &TargetConn{Conn: conn}, nil
}
