package pgconn

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type TargetConn struct {
	*pgx.Conn
}

type Dialer interface {
	Dial(context.Context, *pgx.ConnConfig) (*TargetConn, error)
}

type DirectDialer struct{}

func (DirectDialer) Dial(ctx context.Context, config *pgx.ConnConfig) (*TargetConn, error) {
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	return &TargetConn{Conn: conn}, nil
}
