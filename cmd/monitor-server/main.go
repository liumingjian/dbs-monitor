package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/collect"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/evaluator"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	"github.com/liumingjian/dbs-monitor/internal/notify"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
	webassets "github.com/liumingjian/dbs-monitor/web"
)

const (
	defaultPostgresDataDirectory = "/opt/dbs-monitor/var/lib/postgresql"
	rotateMasterKeyCommand       = "rotate-master-key"
	commandUsage                 = "usage: dbs-monitor-server [--version|rotate-master-key|diagnostic-bundle [output]]"
)

var (
	version   = "0.0.0-dev+unknown"
	commitSHA = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runCommand(ctx, os.Args[1:], os.Stdout); err != nil {
		log.Printf("monitor-server: %v", err)
		os.Exit(1)
	}
}

func runCommand(ctx context.Context, arguments []string, output io.Writer) error {
	switch {
	case len(arguments) == 0:
		return run(ctx)
	case len(arguments) == 1 && arguments[0] == "--version":
		_, err := fmt.Fprintf(output, "dbs-monitor-server %s (%s)\n", version, commitSHA)
		return err
	case len(arguments) == 1 && arguments[0] == rotateMasterKeyCommand:
		return runMasterKeyRotationCommand(ctx)
	case len(arguments) == 1 && arguments[0] == diagnosticBundleCommand:
		return runDiagnosticBundleCommand(ctx, defaultDiagnosticBundleOutput(time.Now().UTC()))
	case len(arguments) == 2 && arguments[0] == diagnosticBundleCommand:
		return runDiagnosticBundleCommand(ctx, arguments[1])
	default:
		return errors.New(commandUsage)
	}
}

func defaultDiagnosticBundleOutput(now time.Time) string {
	return fmt.Sprintf("%s%s%s", diagnosticBundlePrefix, now.UTC().Format("20060102T150405Z"), diagnosticBundleSuffix)
}

func run(ctx context.Context) error {
	startedAt := time.Now().UTC()
	configPath := env("DBS_MONITOR_CONFIG_FILE", defaultServerConfigPath)
	config, configPermissionsSecure, err := loadServerConfig(configPath)
	if err != nil {
		return err
	}
	health := platformhealth.NewStore(version, startedAt, log.Default())
	reportConfigPermissions(health, log.Default(), configPath, configPermissionsSecure, time.Now().UTC())
	certificate, key, err := ensureCertificates(env("CERT_DIR", "certs"), env("PUBLIC_HOST", ""))
	if err != nil {
		return err
	}
	expiresAt, certificateErr := certificateExpiration(certificate)
	if certificateErr != nil {
		health.Update(time.Now().UTC(), platformhealth.CertificateSource(time.Now().UTC(), nil))
		return certificateErr
	}
	health.Update(time.Now().UTC(), platformhealth.CertificateSource(time.Now().UTC(), expiresAt))
	static, err := webassets.Files()
	if err != nil {
		return err
	}

	health.Update(time.Now().UTC(), platformhealth.DatabaseSource(errors.New("platform database startup pending")))
	serverHandler := newSwitchableHandler(securityHeadersHandler(startupFailureHandler(health)))
	server := &http.Server{
		Addr:              env("LISTEN_ADDR", ":8443"),
		Handler:           serverHandler,
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig:         serverTLSConfig(),
	}
	serverErrors := make(chan error, 1)
	go func() {
		err := server.ListenAndServeTLS(certificate, key)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErrors <- err
	}()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	connectionString := config.PlatformDatabaseURL
	credentialDirectory := config.MasterKeyPath
	pool, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		return fmt.Errorf("open platform database: %w", err)
	}
	defer pool.Close()
	platform := &db.Pool{Pool: pool}
	retryDelay := time.Second
	recovering := false
	for {
		ready, err := preparePlatformDatabase(
			ctx, platform, connectionString, credentialDirectory,
			config.MigrationLockWaitTimeout, recovering, health, log.Default(),
		)
		if err != nil {
			return err
		}
		if ready {
			break
		}
		recovering = true
		log.Printf("monitor-server: platform database not ready; retrying startup in %s", retryDelay)
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case err := <-serverErrors:
			timer.Stop()
			return err
		case <-timer.C:
		}
		if retryDelay < 30*time.Second {
			retryDelay *= 2
			if retryDelay > 30*time.Second {
				retryDelay = 30 * time.Second
			}
		}
	}

	processLock, err := acquireServerProcessLock(ctx, pool)
	if err != nil {
		return fmt.Errorf("reserve server process lock: %w", err)
	}
	defer func() {
		if err := processLock.Release(); err != nil {
			log.Printf("monitor-server: %v", err)
		}
	}()
	rotationLock, err := acquireMasterKeyRotationLock(ctx, pool)
	if err != nil {
		return fmt.Errorf("reserve server master key rotation lock: %w", err)
	}
	defer func() {
		if err := rotationLock.Release(); err != nil {
			log.Printf("monitor-server: %v", err)
		}
	}()

	keyring, err := openStartupCredentialKeyring(
		ctx, platform, credentialDirectory, health, log.Default(), time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	notificationSnapshotStore := notify.NewChannelSnapshotStore(notificationSnapshotPath(credentialDirectory))
	if keyring.Fault() == nil {
		if err := notificationSnapshotStore.Sync(ctx, platform); err != nil {
			return fmt.Errorf("initialize notification channel snapshot: %w", err)
		}
	}
	health.SetFailureObserver(func(failure platformhealth.FailureFact) {
		go func() {
			deliveryContext, cancel := context.WithTimeout(ctx, notificationDeliveryTimeout)
			defer cancel()
			if err := sendPlatformUnavailableNotification(deliveryContext, notificationSnapshotStore, keyring, failure); err != nil {
				log.Printf("send platform unavailable notification: %v", err)
			}
		}()
	})
	if err := metric.EnsurePartitionsWithSpan(ctx, platform, time.Now(), config.PartitionSpan); err != nil {
		health.Update(time.Now().UTC(), platformhealth.PartitionSource(platformhealth.PartitionFacts{
			ConsecutiveFailures: 1, PrebuildDaysRemaining: 6,
		}))
		log.Printf("partition creation failed: %v", err)
	} else {
		health.Update(time.Now().UTC(), platformhealth.PartitionSource(platformhealth.PartitionFacts{
			PrebuildDaysRemaining: 7,
		}))
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

	collectionConfig, err := collectionConfigFromEnvironment()
	if err != nil {
		return err
	}
	collector, err := collect.NewWithConfig(platform, monitorpg.DirectDialer{}, clock.Real{}, keyring, collectionConfig)
	if err != nil {
		return fmt.Errorf("collection scheduler config: %w", err)
	}
	collector.SetPlatformHealth(health)
	collector.SetPartitionSpan(config.PartitionSpan)
	diskThresholds, err := diskThresholdsFromEnvironment()
	if err != nil {
		return err
	}
	if err := collector.SetDiskMonitor(env("PGDATA", defaultPostgresDataDirectory), diskThresholds); err != nil {
		return fmt.Errorf("disk monitor config: %w", err)
	}
	refreshPlatformDatabaseHealth(ctx, platform, health, time.Now().UTC())
	health.Update(time.Now().UTC(), platformhealth.SourceSnapshot{
		Source: platformhealth.SourceAgentIngress, Status: platformhealth.StatusOK, Code: "AGENT_INGRESS_READY",
	})
	health.Update(time.Now().UTC(), platformhealth.DiskUnavailableSource(platformhealth.DiskNormal))
	evaluationConfig, err := evaluationConfigFromEnvironment()
	if err != nil {
		return err
	}
	evaluation, err := evaluator.NewWithConfig(platform, clock.Real{}, collector.WithTriggerSnapshotConnection, evaluationConfig)
	if err != nil {
		return fmt.Errorf("alert evaluator config: %w", err)
	}
	go collector.Run(ctx, time.Second)
	go runPartitionMaintenance(ctx, platform, health, config.PartitionSpan, config.PartitionMaintenanceInterval)
	go runAlertHistoryMaintenance(ctx, platform)
	go runNotificationDelivery(ctx, platform, keyring)
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

	distribution, err := httpapi.LoadAgentDistribution(
		filepath.Join(env("CERT_DIR", "certs"), "ca.crt"),
		config.AgentBinaryDir,
	)
	if err != nil {
		return err
	}
	if err := distribution.HealthError(); err != nil {
		log.Printf("Agent distribution unavailable: %v", err)
	}
	sessionConfig := httpapi.SessionConfig{
		AbsoluteTTL: config.SessionAbsoluteTTL,
		IdleTTL:     config.SessionIdleTTL,
	}
	apiHandler := httpapi.NewHandlerWithPlatformHealthAndAgentDistributionAndSessionConfig(
		platform, clock.Real{}, keyring, monitorpg.DirectDialer{}, version, health, distribution,
		sessionConfig,
	)
	apiHandler.SetPartitionSpan(config.PartitionSpan)
	apiHandler.SetNotificationSnapshotStore(notificationSnapshotStore)
	apiRoutes := apiHandler.Routes()
	fileServer := http.FileServer(http.FS(static))
	applicationHandler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if len(request.URL.Path) >= 5 && request.URL.Path[:5] == "/api/" {
			apiRoutes.ServeHTTP(writer, request)
			return
		}
		if _, err := fs.Stat(static, strings.TrimPrefix(request.URL.Path, "/")); err != nil {
			request.URL.Path = "/"
		}
		fileServer.ServeHTTP(writer, request)
	})
	serverHandler.Switch(securityHeadersHandler(platformFailureHandler(applicationHandler, health)))
	select {
	case <-ctx.Done():
		return nil
	case err := <-serverErrors:
		return err
	}
}

func runAlertHistoryMaintenance(ctx context.Context, platform *db.Pool) {
	deleteExpiredAlertHistory := func(now time.Time) {
		if _, err := alerting.DeleteRecoveredAlertHistory(ctx, platform, now); err != nil {
			log.Printf("alert history retention failed: %v", err)
		}
	}
	deleteExpiredAlertHistory(time.Now().UTC())

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			deleteExpiredAlertHistory(now)
		}
	}
}

func runPartitionMaintenance(ctx context.Context, platform *db.Pool, health *platformhealth.Store, span, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastSuccess := time.Now().UTC()
	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := metric.EnsurePartitionsWithSpan(ctx, platform, now, span); err != nil {
				consecutiveFailures++
				health.Update(now, platformhealth.PartitionSource(platformhealth.PartitionFacts{
					ConsecutiveFailures:   consecutiveFailures,
					PrebuildDaysRemaining: partitionDaysRemaining(lastSuccess, now),
				}))
				log.Printf("partition creation failed: %v", err)
				continue
			}
			if err := metric.DropExpiredPartitionsWithSpan(ctx, platform, now, span); err != nil {
				consecutiveFailures++
				health.Update(now, platformhealth.PartitionSource(platformhealth.PartitionFacts{
					ConsecutiveFailures: consecutiveFailures, PrebuildDaysRemaining: 7,
				}))
				log.Printf("partition retention failed: %v", err)
				continue
			}
			if err := collect.DropExpiredStatActivitySnapshots(ctx, platform, now); err != nil {
				consecutiveFailures++
				health.Update(now, platformhealth.PartitionSource(platformhealth.PartitionFacts{
					ConsecutiveFailures: consecutiveFailures, PrebuildDaysRemaining: 7,
				}))
				log.Printf("activity snapshot retention failed: %v", err)
				continue
			}
			if err := collect.DropExpiredQueryStatisticsSnapshots(ctx, platform, now); err != nil {
				consecutiveFailures++
				health.Update(now, platformhealth.PartitionSource(platformhealth.PartitionFacts{
					ConsecutiveFailures: consecutiveFailures, PrebuildDaysRemaining: 7,
				}))
				log.Printf("query statistics snapshot retention failed: %v", err)
				continue
			}
			lastSuccess = now.UTC()
			consecutiveFailures = 0
			health.Update(now, platformhealth.PartitionSource(platformhealth.PartitionFacts{
				PrebuildDaysRemaining: 7,
			}))
		}
	}
}

func partitionDaysRemaining(lastSuccess, now time.Time) int {
	remaining := 7 - int(now.UTC().Sub(lastSuccess.UTC())/(24*time.Hour))
	if remaining < 0 {
		return 0
	}
	return remaining
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

func collectionConfigFromEnvironment() (collect.Config, error) {
	config := collect.DefaultConfig()
	for _, setting := range []struct {
		name   string
		target *int
	}{
		{name: "COLLECT_PROBE_CONCURRENCY", target: &config.ProbeConcurrency},
		{name: "COLLECT_QUERY_CONCURRENCY", target: &config.QueryConcurrency},
	} {
		value := os.Getenv(setting.name)
		if value == "" {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return collect.Config{}, fmt.Errorf("%s must be an integer", setting.name)
		}
		*setting.target = parsed
	}
	return config, nil
}

func diskThresholdsFromEnvironment() (platformhealth.DiskThresholds, error) {
	thresholds := platformhealth.DefaultDiskThresholds()
	for _, setting := range []struct {
		name   string
		target *float64
	}{
		{name: "DISK_WARNING_PERCENT", target: &thresholds.Warning},
		{name: "DISK_CRITICAL_PERCENT", target: &thresholds.Critical},
		{name: "DISK_EMERGENCY_PERCENT", target: &thresholds.Emergency},
	} {
		value := os.Getenv(setting.name)
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return platformhealth.DiskThresholds{}, fmt.Errorf("%s must be a number", setting.name)
		}
		*setting.target = parsed
	}
	if err := thresholds.Validate(); err != nil {
		return platformhealth.DiskThresholds{}, fmt.Errorf("disk thresholds: %w", err)
	}
	return thresholds, nil
}

func evaluationConfigFromEnvironment() (evaluator.Config, error) {
	const setting = "ALERT_TRIGGER_SNAPSHOT_SESSION_LIMIT"
	config := evaluator.DefaultConfig()
	value := os.Getenv(setting)
	if value == "" {
		return config, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return evaluator.Config{}, fmt.Errorf("%s must be an integer", setting)
	}
	config.TriggerSnapshotSessionLimit = parsed
	if err := config.Validate(); err != nil {
		return evaluator.Config{}, fmt.Errorf("%s: %w", setting, err)
	}
	return config, nil
}
