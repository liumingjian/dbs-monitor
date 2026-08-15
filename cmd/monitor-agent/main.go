package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/agent"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

const commandUsage = "usage: dbs-monitor-agent [--version]"

var (
	version   = "0.0.0-dev+unknown"
	commitSHA = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runCommand(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCommand(ctx context.Context, arguments []string, output io.Writer) error {
	switch {
	case len(arguments) == 0:
		return run(ctx)
	case len(arguments) == 1 && arguments[0] == "--version":
		_, err := fmt.Fprintf(output, "dbs-monitor-agent %s (%s)\n", version, commitSHA)
		return err
	default:
		return errors.New(commandUsage)
	}
}

func run(ctx context.Context) error {
	agentToken, err := configuredAgentToken()
	if err != nil {
		return err
	}
	cfg, err := agent.ParseConfig(os.Getenv("SERVER_URL"), os.Getenv("INSTANCE_ID"), agentToken, os.Getenv("CA_FILE"))
	if err != nil {
		return err
	}
	client, err := newClient(cfg)
	if err != nil {
		return err
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
			return nil
		case <-ticker.C:
		}
	}
}

func configuredAgentToken() (string, error) {
	if path := os.Getenv("AGENT_TOKEN_FILE"); path != "" {
		contents, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read Agent token file: %w", err)
		}
		return strings.TrimSpace(string(contents)), nil
	}
	return os.Getenv("AGENT_TOKEN"), nil
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
