package internal_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestAcceptanceWorkflowIsIndependentAndBlocking(t *testing.T) {
	contents := readWorkflow(t, "acceptance.yml")
	for _, required := range []string{
		"name: acceptance", "push:", "      - main", "workflow_dispatch:",
		"runs-on: ubuntu-latest", "run: make acceptance", "if: always()",
		"results/acceptance-result.json",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("acceptance workflow is missing %q", required)
		}
	}
	if strings.Contains(contents, "make check-full") {
		t.Fatal("acceptance workflow is not independent from check-full")
	}

	checkFull := readWorkflow(t, "check-full.yml")
	if !strings.Contains(checkFull, "run: make _check-full") {
		t.Fatal("check-full workflow does not run the CI core independently")
	}
	if strings.Contains(checkFull, "run: make check-full") || strings.Contains(checkFull, "run: make acceptance") {
		t.Fatal("check-full workflow invokes acceptance")
	}
}

func TestReleaseEvidenceWorkflowIsPinnedToManualSHA(t *testing.T) {
	contents := readWorkflow(t, "release-evidence.yml")
	for _, required := range []string{
		"name: release-evidence", "workflow_dispatch:", "sha:", "required: true",
		"ref: ${{ inputs.sha }}", "CANDIDATE_SHA: ${{ inputs.sha }}",
		"run: make release-evidence", "results/pg-range-evidence.json",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("release-evidence workflow is missing %q", required)
		}
	}
	if strings.Contains(contents, "push:") || strings.Contains(contents, "schedule:") {
		t.Fatal("release-evidence must only be dispatched manually")
	}
	for _, forbidden := range []string{"govulncheck", "npm audit", "rt_c", "vulnerability"} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("release evidence includes non-evidence concern %q", forbidden)
		}
	}
}

func TestReleaseGateValidatesExistingEvidenceWithoutBuilding(t *testing.T) {
	contents := readWorkflow(t, "release-gate.yml")
	for _, required := range []string{
		"name: release-gate", "tags:", "      - 'v*'", "workflow_dispatch:",
		"actions: read", "checks: read", "getRef", "getTag", "listWorkflowRuns",
		"listWorkflowRunArtifacts", "docs/validation", "verdict", "GO", "NO-GO",
		"PROVISIONAL-PASS", "check-full.yml", "vulnerability-reports-",
		"actions/download-artifact@v4", "run-id:", "results/vulnerability-reports/govulncheck.txt",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("release-gate workflow is missing %q", required)
		}
	}
	for _, forbidden := range []string{"make build", "go build", "go run golang.org/x/vuln", "npm audit"} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("release-gate reruns work via %q", forbidden)
		}
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
	if !strings.Contains(vulnerabilities, "npm audit") || !strings.Contains(vulnerabilities, "||") {
		t.Fatal("npm audit must be report-only")
	}

	workflow := readWorkflow(t, "check-full.yml")
	for _, required := range []string{"if: always()", "vulnerability-reports-${{ github.sha }}", "results/govulncheck.txt", "results/npm-audit.json"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("check-full workflow is missing vulnerability artifact field %q", required)
		}
	}
}

func TestMainBranchProtectionPolicy(t *testing.T) {
	path := filepath.Join(internalRoot(t), "..", ".github", "branch-protection.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read branch protection policy: %v", err)
	}
	var policy struct {
		RequiredStatusChecks struct {
			Strict   bool     `json:"strict"`
			Contexts []string `json:"contexts"`
		} `json:"required_status_checks"`
		EnforceAdmins              bool `json:"enforce_admins"`
		RequiredPullRequestReviews any  `json:"required_pull_request_reviews"`
		AllowForcePushes           bool `json:"allow_force_pushes"`
		AllowDeletions             bool `json:"allow_deletions"`
	}
	if err := json.Unmarshal(contents, &policy); err != nil {
		t.Fatalf("parse branch protection policy: %v", err)
	}
	if !policy.RequiredStatusChecks.Strict || len(policy.RequiredStatusChecks.Contexts) != 1 || policy.RequiredStatusChecks.Contexts[0] != "check" {
		t.Errorf("required checks = strict:%v contexts:%v, want only strict check", policy.RequiredStatusChecks.Strict, policy.RequiredStatusChecks.Contexts)
	}
	if !policy.EnforceAdmins {
		t.Error("branch protection permits administrator bypass")
	}
	if policy.RequiredPullRequestReviews != nil {
		t.Error("branch protection unexpectedly requires reviews")
	}
	if policy.AllowForcePushes || policy.AllowDeletions {
		t.Error("branch protection permits force pushes or deletion")
	}
}

func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(internalRoot(t), "..", ".github", "workflows", name))
	if err != nil {
		t.Fatalf("read workflow %s: %v", name, err)
	}
	var workflow any
	if err := yaml.Unmarshal(contents, &workflow); err != nil {
		t.Fatalf("parse workflow %s: %v", name, err)
	}
	return string(contents)
}
