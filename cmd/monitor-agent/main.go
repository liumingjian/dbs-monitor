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
	"github.com/shirou/gopsutil/v4/cpu"
)

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
	for {
		if err := runOnce(ctx, client, cfg, time.Now().UTC()); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runOnce(ctx context.Context, client *api.ClientWithResponses, cfg agent.Config, now time.Time) error {
	values, err := cpu.PercentWithContext(ctx, 100*time.Millisecond, false)
	if err != nil {
		return fmt.Errorf("collect CPU usage: %w", err)
	}
	if len(values) != 1 {
		return fmt.Errorf("collect CPU usage: got %d aggregate values", len(values))
	}
	body := api.AgentReport{InstanceId: cfg.Instance, Timestamp: now}
	body.Metrics = append(body.Metrics, struct {
		Metric api.AgentReportMetricsMetric `json:"metric"`
		Value  float32                      `json:"value"`
	}{Metric: api.AgentReportMetricsMetricHostCpuUsagePercent, Value: float32(values[0])})
	response, err := client.ReportAgentMetricsWithResponse(ctx, body)
	if err != nil {
		return fmt.Errorf("report Agent metrics: %w", err)
	}
	if response.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("report Agent metrics: server returned %s", response.Status())
	}
	return nil
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
