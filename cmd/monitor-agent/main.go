package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/agent"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

var version = "1.0.0"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg, err := agent.ParseConfig(os.Getenv("SERVER_URL"), os.Getenv("INSTANCE_ID"), os.Getenv("AGENT_TOKEN"), os.Getenv("CA_FILE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	client, err := newClient(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	dataPath := os.Getenv("PGDATA")
	if dataPath == "" {
		dataPath = "/"
	}
	service := agent.NewService(client, cfg, version, dataPath)
	for {
		if err := service.RunOnce(ctx, time.Now().UTC()); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func newClient(cfg agent.Config) (*api.ClientWithResponses, error) {
	ca, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, fmt.Errorf("CA file contains no certificates")
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	return api.NewClientWithResponses(cfg.ServerURL,
		api.WithHTTPClient(httpClient),
		api.WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
			request.Header.Set("Authorization", "Bearer "+cfg.Token)
			return nil
		}),
	)
}
