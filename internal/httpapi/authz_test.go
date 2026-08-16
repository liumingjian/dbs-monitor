package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
)

func TestPlatformHealthEndpointIsRegistered(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/health", nil)
	response := httptest.NewRecorder()

	httpapi.NewHandler(nil, nil, nil).Routes().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated platform health status = %d, want 401", response.Code)
	}
}

func TestDiagnosticAPIIsCompleteAndReadOnly(t *testing.T) {
	doc := loadSpec(t)
	paths := []string{
		"/api/v1/diagnostics/health",
		"/api/v1/diagnostics/disk",
		"/api/v1/diagnostics/scheduler",
		"/api/v1/diagnostics/partitions",
		"/api/v1/diagnostics/certificate",
		"/api/v1/diagnostics/keyring",
		"/api/v1/diagnostics/platform",
	}
	for _, path := range paths {
		item := doc.Paths.Find(path)
		if item == nil || item.Get == nil {
			t.Errorf("diagnostic endpoint GET %s is not declared", path)
			continue
		}
		if item.Get.Extensions["x-required-role"] != "PLATFORM_ADMIN" {
			t.Errorf("diagnostic endpoint GET %s role = %v, want PLATFORM_ADMIN", path, item.Get.Extensions["x-required-role"])
		}
		if len(item.Operations()) != 1 {
			t.Errorf("diagnostic endpoint %s exposes methods other than GET", path)
		}
	}
}

func TestEveryOperationDeclaresRequiredRole(t *testing.T) {
	doc := loadSpec(t)
	allowedRoles := map[string]bool{
		"READONLY":       true,
		"ALERT_ADMIN":    true,
		"PLATFORM_ADMIN": true,
		"AGENT":          true,
	}

	operationLocations := map[string]string{}
	specRoles := map[string]string{}
	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			if operation.OperationID == "" {
				t.Errorf("%s %s has no operationId", method, path)
				continue
			}
			if previousLocation, exists := operationLocations[operation.OperationID]; exists {
				t.Errorf("duplicate operationId %q on %s %s and %s", operation.OperationID, method, path, previousLocation)
			}
			operationLocations[operation.OperationID] = fmt.Sprintf("%s %s", method, path)

			value, ok := operation.Extensions["x-required-role"]
			if !ok {
				t.Errorf("operation %q has no x-required-role", operation.OperationID)
				continue
			}
			role, ok := value.(string)
			if !ok || !allowedRoles[role] {
				t.Errorf("operation %q has invalid x-required-role %v", operation.OperationID, value)
				continue
			}
			if role == "AGENT" && (operation.Security == nil || len(*operation.Security) == 0) {
				t.Errorf("agent operation %q has no bearer security", operation.OperationID)
			}
			operationID := strings.ToUpper(operation.OperationID[:1]) + operation.OperationID[1:]
			specRoles[operationID] = role
		}
	}
	if len(operationLocations) == 0 {
		t.Fatal("spec has no operations")
	}
	if !reflect.DeepEqual(specRoles, httpapi.RequiredRoles) {
		t.Fatalf("runtime role table = %v, spec roles = %v", httpapi.RequiredRoles, specRoles)
	}
}

func TestResponseSchemasDoNotExposeSecrets(t *testing.T) {
	doc := loadSpec(t)
	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			for status, responseRef := range operation.Responses.Map() {
				if responseRef == nil || responseRef.Value == nil {
					continue
				}
				allowedTopLevelSecrets := responseSecretAllowlist(method, path, status)
				for mediaType, media := range responseRef.Value.Content {
					if media.Schema == nil {
						continue
					}
					location := fmt.Sprintf("%s %s response %s %s", method, path, status, mediaType)
					assertNoSecretProperties(t, location, media.Schema, map[*openapi3.Schema]bool{}, allowedTopLevelSecrets)
				}
			}
		}
	}
}

func TestResponseSecretAllowlistIsEndpointSpecific(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		status string
		want   map[string]bool
	}{
		{
			name:   "created user exposes only its initial password",
			method: "POST",
			path:   "/api/v1/users",
			status: "201",
			want:   map[string]bool{"initial_password": true},
		},
		{
			name:   "password reset exposes only the generated password",
			method: "POST",
			path:   "/api/v1/users/{id}/password",
			status: "200",
			want:   map[string]bool{"password": true},
		},
		{
			name:   "instance creation exposes no secret",
			method: "POST",
			path:   "/api/v1/instances",
			status: "201",
		},
		{
			name:   "agent registration exposes only its one-time token",
			method: "POST",
			path:   "/api/v1/instances/{id}/agent/registration",
			status: "200",
			want:   map[string]bool{"agent_token": true},
		},
		{
			name:   "agent token rotation exposes only its one-time token",
			method: "POST",
			path:   "/api/v1/instances/{id}/agent/token/rotation",
			status: "200",
			want:   map[string]bool{"agent_token": true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := responseSecretAllowlist(test.method, test.path, test.status); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("allowlist = %v, want %v", got, test.want)
			}
		})
	}
}

func responseSecretAllowlist(method, path, status string) map[string]bool {
	switch {
	case method == "POST" && status == "201" && path == "/api/v1/users":
		return map[string]bool{"initial_password": true}
	case method == "POST" && status == "200" && path == "/api/v1/users/{id}/password":
		return map[string]bool{"password": true}
	case method == "POST" && status == "200" && (path == "/api/v1/instances/{id}/agent/registration" || path == "/api/v1/instances/{id}/agent/token/rotation"):
		return map[string]bool{"agent_token": true}
	default:
		return nil
	}
}

func assertNoSecretProperties(t *testing.T, location string, ref *openapi3.SchemaRef, visited map[*openapi3.Schema]bool, allowedTopLevelSecrets map[string]bool) {
	t.Helper()
	if ref == nil || ref.Value == nil || visited[ref.Value] {
		return
	}
	visited[ref.Value] = true

	for name, property := range ref.Value.Properties {
		if allowedTopLevelSecrets[name] {
			continue
		}
		lower := strings.ToLower(name)
		for _, forbidden := range []string{"password", "secret", "token", "credential", "dsn"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s exposes forbidden response property %q", location, name)
			}
		}
		assertNoSecretProperties(t, location+"."+name, property, visited, nil)
	}
	assertNoSecretProperties(t, location+"[]", ref.Value.Items, visited, nil)
	for _, child := range append(append(ref.Value.AllOf, ref.Value.OneOf...), ref.Value.AnyOf...) {
		assertNoSecretProperties(t, location, child, visited, allowedTopLevelSecrets)
	}
}

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	path := filepath.Join(cwd, "..", "..", "api", "openapi.bundled.yaml")
	doc, err := openapi3.NewLoader().LoadFromFile(path)
	if err != nil {
		t.Fatalf("load OpenAPI spec: %v", err)
	}
	return doc
}
