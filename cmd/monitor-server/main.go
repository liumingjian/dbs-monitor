package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/collect"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/evaluator"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/migrations"
	webassets "github.com/liumingjian/dbs-monitor/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Printf("monitor-server: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	connectionString := env("DATABASE_URL", "postgres:///dbs_monitor?host=/opt/dbs-monitor/run&sslmode=disable")
	credentialDirectory := env("CREDENTIALS_DIR", "/opt/dbs-monitor/etc/credentials")
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		return fmt.Errorf("open platform database: %w", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}

	migrationDB, err := migrations.Open(connectionString)
	if err != nil {
		return err
	}
	if _, err := migrations.Up(ctx, migrationDB, credentialDirectory); err != nil {
		migrationDB.Close()
		return err
	}
	migrationDB.Close()
	keyring, err := instance.OpenCredentialKeyring(credentialDirectory, true)
	if err != nil {
		return err
	}
	if err := metric.EnsurePartitions(ctx, platform, time.Now()); err != nil {
		return err
	}
	adminExists, err := httpapi.AdminExists(ctx, platform)
	if err != nil {
		return err
	}
	if !adminExists {
		adminPassword := env("INITIAL_ADMIN_PASSWORD", "")
		if adminPassword == "" {
			adminPassword, err = randomPassword()
			if err != nil {
				return err
			}
			log.Printf("initial administrator password (shown once): %s", adminPassword)
		}
		if err := httpapi.SeedAdmin(ctx, platform, "admin", adminPassword); err != nil {
			return err
		}
	}

	collector := collect.New(platform, monitorpg.DirectDialer{}, clock.Real{}, keyring)
	evaluation := evaluator.New(platform, clock.Real{})
	go collector.Run(ctx, time.Second)
	go runPartitionMaintenance(ctx, platform)
	go func() {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			evaluation.Run(ctx, 2*time.Second)
		}
	}()

	static, err := webassets.Files()
	if err != nil {
		return err
	}
	apiHandler := httpapi.NewHandler(platform, clock.Real{}, keyring).Routes()
	fileServer := http.FileServer(http.FS(static))
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if len(request.URL.Path) >= 5 && request.URL.Path[:5] == "/api/" {
			apiHandler.ServeHTTP(writer, request)
			return
		}
		if _, err := fs.Stat(static, strings.TrimPrefix(request.URL.Path, "/")); err != nil {
			request.URL.Path = "/"
		}
		fileServer.ServeHTTP(writer, request)
	})
	server := &http.Server{Addr: env("LISTEN_ADDR", ":8443"), Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	certificate, key, err := ensureCertificates(env("CERT_DIR", "certs"), env("PUBLIC_HOST", ""))
	if err != nil {
		return err
	}
	server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if err := server.ListenAndServeTLS(certificate, key); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runPartitionMaintenance(ctx context.Context, platform *db.Pool) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := metric.EnsurePartitions(ctx, platform, now); err != nil {
				log.Printf("partition creation failed: %v", err)
				continue
			}
			if err := metric.DropExpiredPartitions(ctx, platform, now); err != nil {
				log.Printf("partition retention failed: %v", err)
			}
		}
	}
}

func randomPassword() (string, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
