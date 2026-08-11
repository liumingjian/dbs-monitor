package httpapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	pgxconn "github.com/jackc/pgx/v5/pgconn"

	"github.com/liumingjian/dbs-monitor/internal/api"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
)

const minimumTargetVersionNum = 130000

type targetConnectionInput struct {
	host     string
	port     int
	database string
	username string
	password string
}

type targetValidationError struct {
	code    api.ErrorErrorCode
	message string
}

func (validationError *targetValidationError) Error() string {
	return validationError.message
}

func targetVersionSupported(serverVersionNum int) bool {
	return serverVersionNum >= minimumTargetVersionNum
}

func validateTargetConnection(ctx context.Context, dialer monitorpg.Dialer, input targetConnectionInput) error {
	config, err := pgx.ParseConfig("postgres://localhost/?sslmode=disable")
	if err != nil {
		return err
	}
	config.Host = input.host
	config.Port = uint16(input.port)
	config.Database = input.database
	config.User = input.username
	config.Password = input.password
	config.RuntimeParams["application_name"] = "dbs-monitor-onboarding"

	validationContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	connection, err := dialer.Dial(validationContext, config)
	if err != nil {
		return classifyTargetConnectionError(err)
	}
	defer connection.Close(context.Background())

	var serverVersionNum int
	if err := connection.QueryRow(validationContext, "SELECT current_setting('server_version_num')::integer").Scan(&serverVersionNum); err != nil {
		return classifyTargetConnectionError(err)
	}
	if !targetVersionSupported(serverVersionNum) {
		return &targetValidationError{code: api.ONBOARDINGVERSIONUNSUPPORTED, message: "目标 PostgreSQL 版本低于 13"}
	}
	return nil
}

func classifyTargetConnectionError(err error) *targetValidationError {
	var postgresError *pgxconn.PgError
	if errors.As(err, &postgresError) && strings.HasPrefix(postgresError.Code, "28") {
		return &targetValidationError{code: api.AUTHFAILED, message: "目标 PostgreSQL 认证失败"}
	}
	return &targetValidationError{code: api.NETWORKUNREACHABLE, message: "无法连接目标 PostgreSQL"}
}
