package internal_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseGateMakeTargets(t *testing.T) {
	root := filepath.Join(internalRoot(t), "..")
	contents, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(contents)
	check := requireMakeTarget(t, makefile, "check")
	if strings.Contains(check, "acceptance") {
		t.Fatal("make check must not invoke acceptance")
	}
	checkFull := requireMakeTarget(t, makefile, "check-full")
	for _, required := range []string{"_check-full", "acceptance"} {
		if !strings.Contains(checkFull, required) {
			t.Errorf("check-full does not invoke %q", required)
		}
	}
	fullCore := requireMakeTarget(t, makefile, "_check-full")
	for _, required := range []string{"$(MAKE) build", "$(MAKE) check-vulnerabilities"} {
		if !strings.Contains(fullCore, required) {
			t.Errorf("_check-full is missing %q", required)
		}
	}
	build := requireMakeTarget(t, makefile, "build")
	if !strings.Contains(build, "sha256sum dbs-monitor-server dbs-monitor-agent > SHA256SUMS") {
		t.Fatal("make build does not emit SHA256SUMS")
	}
}

func TestCheckFullOwnsVulnerabilityReports(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join(internalRoot(t), "..", "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	vulnerabilities := requireMakeTarget(t, string(makefile), "check-vulnerabilities")
	for _, required := range []string{"govulncheck", "npm audit", "npm-audit.json", "govulncheck.txt"} {
		if !strings.Contains(vulnerabilities, required) {
			t.Errorf("check-vulnerabilities is missing %q", required)
		}
	}
	if !strings.Contains(vulnerabilities, "(cd web && npm audit --json > ../results/npm-audit.json) ||") {
		t.Fatal("npm audit must be report-only")
	}
}
