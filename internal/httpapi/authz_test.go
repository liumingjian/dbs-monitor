package httpapi_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/liumingjian/dbs-monitor/internal/httpapi"
)

func TestEveryOperationDeclaresRequiredRole(t *testing.T) {
	doc := loadSpec(t)
	allowed := map[string]bool{
		"READONLY":       true,
		"ALERT_ADMIN":    true,
		"PLATFORM_ADMIN": true,
		"AGENT":          true,
	}

	seen := map[string]string{}
	specRoles := map[string]string{}
	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			if operation.OperationID == "reportAgentMetrics" && (operation.Security == nil || len(*operation.Security) == 0) {
				t.Errorf("agent operation %q has no bearer security", operation.OperationID)
			}
			if operation.OperationID == "" {
				t.Errorf("%s %s has no operationId", method, path)
				continue
			}
			if previous, exists := seen[operation.OperationID]; exists {
				t.Errorf("duplicate operationId %q on %s %s and %s", operation.OperationID, method, path, previous)
			}
			seen[operation.OperationID] = fmt.Sprintf("%s %s", method, path)

			value, ok := operation.Extensions["x-required-role"]
			if !ok {
				t.Errorf("operation %q has no x-required-role", operation.OperationID)
				continue
			}
			role, ok := value.(string)
			if !ok || !allowed[role] {
				t.Errorf("operation %q has invalid x-required-role %v", operation.OperationID, value)
				continue
			}
			operationID := operation.OperationID
			if operationID != "" {
				operationID = strings.ToUpper(operationID[:1]) + operationID[1:]
			}
			specRoles[operationID] = role
		}
	}
	if len(seen) == 0 {
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
				for mediaType, media := range responseRef.Value.Content {
					if media.Schema != nil {
						allowedSecrets := responseSecretAllowlist(method, path, status)
						assertNoSecretProperties(t, fmt.Sprintf("%s %s response %s %s", method, path, status, mediaType), media.Schema, map[*openapi3.Schema]bool{}, allowedSecrets)
					}
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
	default:
		return nil
	}
}

func assertNoSecretProperties(t *testing.T, location string, ref *openapi3.SchemaRef, visited map[*openapi3.Schema]bool, allowedSecrets map[string]bool) {
	t.Helper()
	if ref == nil || ref.Value == nil || visited[ref.Value] {
		return
	}
	visited[ref.Value] = true

	for name, property := range ref.Value.Properties {
		if allowedSecrets[name] {
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
		assertNoSecretProperties(t, location, child, visited, allowedSecrets)
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
