package internal_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestPlatformDatabasePreflightRunsBeforeMigrations(t *testing.T) {
	mainSource, err := os.ReadFile(filepath.Join(internalRoot(t), "..", "cmd", "monitor-server", "main.go"))
	if err != nil {
		t.Fatalf("read monitor server startup: %v", err)
	}
	contents := string(mainSource)
	preflight := strings.Index(contents, "platformdb.Check(ctx, platform)")
	migration := strings.Index(contents, "migrations.Up(ctx, migrationDB, credentialDirectory)")
	if preflight < 0 || migration < 0 || preflight >= migration {
		t.Fatalf("platform database preflight must run after connection and before migrations")
	}
}

func TestComposeAcceptanceProfilesAndNonRootServer(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join(internalRoot(t), "..", "compose.yaml"))
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	var compose struct {
		Services map[string]struct {
			Image       string         `yaml:"image"`
			Profiles    []string       `yaml:"profiles"`
			User        string         `yaml:"user"`
			Healthcheck map[string]any `yaml:"healthcheck"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(contents, &compose); err != nil {
		t.Fatalf("parse compose.yaml: %v", err)
	}

	required := map[string]struct {
		profile string
		image   string
	}{
		"monitored-pg12": {profile: "target-pg12", image: "postgres:12"},
		"platform-pg16":  {profile: "platform-pg16", image: "postgres:16"},
		"restore-target": {profile: "restore", image: "postgres:17"},
		"smtp-sink":      {profile: "smtp", image: "axllent/mailpit:v1.30.7"},
		"webhook-sink":   {profile: "webhook", image: "node:22-alpine"},
	}
	for name, want := range required {
		service, exists := compose.Services[name]
		if !exists {
			t.Errorf("compose service %s is missing", name)
			continue
		}
		if service.Image != want.image {
			t.Errorf("compose service %s image = %q, want %q", name, service.Image, want.image)
		}
		if len(service.Profiles) != 1 || service.Profiles[0] != want.profile {
			t.Errorf("compose service %s profiles = %v, want [%s]", name, service.Profiles, want.profile)
		}
		if len(service.Healthcheck) == 0 {
			t.Errorf("compose service %s has no healthcheck for up --wait", name)
		}
	}

	server, exists := compose.Services["server"]
	if !exists {
		t.Fatal("compose server service is missing")
	}
	uid := strings.SplitN(server.User, ":", 2)[0]
	if uid == "" || uid == "0" || uid == "root" {
		t.Fatalf("compose server user = %q, want an explicit non-root uid", server.User)
	}
}

func TestAcceptanceCertificateFixturesHaveRealValidityWindows(t *testing.T) {
	fixtures := filepath.Join(internalRoot(t), "..", "test", "acceptance", "fixtures")
	expired := readCertificateFixture(t, fixtures, "tls-expired")
	if !expired.NotAfter.Before(time.Now().UTC()) {
		t.Fatalf("expired fixture NotAfter = %s, want a date in the past", expired.NotAfter)
	}

	expiring := readCertificateFixture(t, fixtures, "tls-expiring-20d")
	validity := expiring.NotAfter.Sub(expiring.NotBefore)
	if validity < 20*24*time.Hour || validity > 21*24*time.Hour {
		t.Fatalf("20-day fixture validity = %s, want 20d..21d", validity)
	}
}

func readCertificateFixture(t *testing.T, directory, name string) *x509.Certificate {
	t.Helper()
	certificatePEM, err := os.ReadFile(filepath.Join(directory, name+".crt"))
	if err != nil {
		t.Fatalf("read %s certificate: %v", name, err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(directory, name+".key"))
	if err != nil {
		t.Fatalf("read %s key: %v", name, err)
	}
	if _, err := tls.X509KeyPair(certificatePEM, keyPEM); err != nil {
		t.Fatalf("load %s certificate pair: %v", name, err)
	}
	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		t.Fatalf("decode %s certificate PEM", name)
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse %s certificate: %v", name, err)
	}
	return certificate
}
