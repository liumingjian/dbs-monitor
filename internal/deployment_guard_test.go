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
	serverDirectory := filepath.Join(internalRoot(t), "..", "cmd", "monitor-server")
	mainSource, err := os.ReadFile(filepath.Join(serverDirectory, "main.go"))
	if err != nil {
		t.Fatalf("read monitor server startup: %v", err)
	}
	startupSource, err := os.ReadFile(filepath.Join(serverDirectory, "startup.go"))
	if err != nil {
		t.Fatalf("read shared platform database startup: %v", err)
	}
	contents := string(startupSource)
	preflight := strings.Index(contents, "platformdb.Check(ctx, platform)")
	migration := strings.Index(contents, "migrations.UpWithLockTimeout(ctx, migrationDB, credentialDirectory, lockWaitTimeout)")
	if preflight < 0 || migration < 0 || preflight >= migration {
		t.Fatalf("platform database preflight must run after connection and before migrations")
	}
	if strings.Count(contents, "platformdb.Check(ctx, platform)") != 1 ||
		!strings.Contains(string(mainSource), "preparePlatformDatabase(") {
		t.Fatal("startup and recovery must share the single platform database prerequisite implementation")
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

func TestAcceptanceRoleFixturesAreHarnessConsumable(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join(internalRoot(t), "..", "test", "acceptance", "fixtures", "roles.yaml"))
	if err != nil {
		t.Fatalf("read role fixtures: %v", err)
	}
	var fixtures struct {
		LongLived map[string]struct {
			Username string `yaml:"username"`
			Role     string `yaml:"role"`
		} `yaml:"long_lived"`
		Disposable struct {
			UsernamePattern  string `yaml:"username_pattern"`
			CreationEndpoint string `yaml:"creation_endpoint"`
		} `yaml:"disposable"`
	}
	if err := yaml.Unmarshal(contents, &fixtures); err != nil {
		t.Fatalf("parse role fixtures: %v", err)
	}

	wantRoles := map[string]string{
		"platform_admin": "PLATFORM_ADMIN",
		"alert_admin":    "ALERT_ADMIN",
		"viewer":         "READONLY",
	}
	seenUsernames := make(map[string]bool, len(wantRoles))
	for fixtureName, wantRole := range wantRoles {
		fixture, exists := fixtures.LongLived[fixtureName]
		if !exists {
			t.Errorf("long-lived role fixture %q is missing", fixtureName)
			continue
		}
		if fixture.Role != wantRole {
			t.Errorf("role fixture %q role = %q, want %q", fixtureName, fixture.Role, wantRole)
		}
		if fixture.Username == "" || seenUsernames[fixture.Username] {
			t.Errorf("role fixture %q username = %q, want a unique non-empty username", fixtureName, fixture.Username)
		}
		seenUsernames[fixture.Username] = true
	}
	if len(fixtures.LongLived) != len(wantRoles) {
		t.Errorf("long-lived role fixture count = %d, want %d", len(fixtures.LongLived), len(wantRoles))
	}
	if !strings.HasPrefix(fixtures.Disposable.UsernamePattern, "{entry_id}-") {
		t.Errorf("disposable username pattern = %q, want an entry ID prefix", fixtures.Disposable.UsernamePattern)
	}
	if fixtures.Disposable.CreationEndpoint != "POST /api/v1/users" {
		t.Errorf("disposable creation endpoint = %q, want the user-management API", fixtures.Disposable.CreationEndpoint)
	}
}

func TestDeliveryAndOperationsPackagingContract(t *testing.T) {
	root := filepath.Join(internalRoot(t), "..")
	readFile := func(path string) string {
		t.Helper()
		contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(contents)
	}
	requirePhrases := func(path string, required ...string) {
		t.Helper()
		contents := readFile(path)
		for _, phrase := range required {
			if !strings.Contains(contents, phrase) {
				t.Errorf("%s is missing %q", path, phrase)
			}
		}
	}

	requirePhrases("packaging/systemd/dbs-monitor-server.service", "User=dbsmon", "ExecStart=/opt/dbs-monitor/bin/dbs-monitor-server")
	requirePhrases("packaging/systemd/dbs-monitor-agent.service", "User=dbs-monitor-agent", "AGENT_TOKEN_FILE", "ExecStart=/opt/dbs-monitor-agent/bin/dbs-monitor-agent")

	for _, path := range []string{"config/server-minimal.yaml", "config/server-full.yaml"} {
		var config map[string]yaml.Node
		if err := yaml.Unmarshal([]byte(readFile(path)), &config); err != nil {
			t.Errorf("parse %s: %v", path, err)
		}
		if _, exists := config["platform_database_url"]; !exists {
			t.Errorf("%s has no platform_database_url", path)
		}
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
