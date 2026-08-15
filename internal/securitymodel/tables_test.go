package securitymodel

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestA10PageAndWriteDecisionsAreComplete(t *testing.T) {
	root := repositoryRoot(t)
	routeRoot := filepath.Join(root, "web", "src", "routes")
	doc, err := openapi3.NewLoader().LoadFromFile(filepath.Join(root, "api", "openapi.bundled.yaml"))
	if err != nil {
		t.Fatalf("load OpenAPI: %v", err)
	}
	registeredPages := map[string]PageAuthorization{}
	sourcePages := map[string][]string{}
	for _, page := range PageAuthorizations {
		if _, exists := registeredPages[page.Path]; exists {
			t.Fatalf("A10 contains duplicate page %q", page.Path)
		}
		registeredPages[page.Path] = page
		if page.Visible != (RoleDecision{Readonly: true, AlertAdmin: true, PlatformAdmin: true}) {
			t.Errorf("A10 page %q visibility = %+v, want all roles", page.Path, page.Visible)
		}
		for _, source := range page.Sources {
			sourcePages[source] = append(sourcePages[source], page.Path)
		}
	}

	discoveredPages, discoveredWrites := discoverWebSurfaces(t, root, routeRoot)
	for path, source := range discoveredPages {
		page, exists := registeredPages[path]
		if !exists {
			t.Errorf("page %q in %s is missing from A10", path, source)
			continue
		}
		if !contains(page.Sources, source) {
			t.Errorf("A10 page %q does not name its route source %s", path, source)
		}
	}
	for path := range registeredPages {
		if path == AuthenticatedShell {
			continue
		}
		if _, exists := discoveredPages[path]; !exists {
			t.Errorf("A10 page %q does not exist in the route tree", path)
		}
	}

	registeredWrites := map[string]PageWrite{}
	for _, write := range PageWrites {
		key := writeKey(write.PagePath, write.Method, write.APIPath)
		if _, exists := registeredWrites[key]; exists {
			t.Fatalf("A10 contains duplicate write %s", key)
		}
		registeredWrites[key] = write
		page, exists := registeredPages[write.PagePath]
		if !exists {
			t.Errorf("A10 write %s names unknown page", key)
			continue
		}
		if !writeExistsInSources(discoveredWrites, page.Sources, write.Method, write.APIPath) {
			t.Errorf("A10 write %s does not exist in page sources %v", key, page.Sources)
		}
		assertWriteRoleMatchesOpenAPI(t, doc, write)
	}
	for _, write := range discoveredWrites {
		pages := sourcePages[write.Source]
		if len(pages) == 0 {
			t.Errorf("write %s %s in %s is not owned by an A10 page", write.Method, write.APIPath, write.Source)
			continue
		}
		for _, page := range pages {
			key := writeKey(page, write.Method, write.APIPath)
			if _, exists := registeredWrites[key]; !exists {
				t.Errorf("page write %s is missing from A10", key)
			}
		}
	}
}

type discoveredWrite struct {
	Source  string
	Method  string
	APIPath string
}

func discoverWebSurfaces(t *testing.T, root, routeRoot string) (map[string]string, []discoveredWrite) {
	t.Helper()
	pagePattern := regexp.MustCompile(`(?m)^\s*path:\s*'([^']+)'`)
	mutationPattern := regexp.MustCompile(`\$api\.useMutation\('([^']+)',\s*'([^']+)'\)`)
	fetchPattern := regexp.MustCompile(`(?s)fetch\('([^']+)'[^)]{0,400}?method:\s*'([^']+)'`)
	pages := map[string]string{}
	writes := []discoveredWrite{}
	err := filepath.WalkDir(routeRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || (!strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx")) || strings.Contains(path, ".test.") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		source = filepath.ToSlash(source)
		for _, match := range pagePattern.FindAllSubmatch(contents, -1) {
			page := string(match[1])
			if previous, exists := pages[page]; exists {
				return fmt.Errorf("duplicate page %q in %s and %s", page, previous, source)
			}
			pages[page] = source
		}
		for _, match := range mutationPattern.FindAllSubmatch(contents, -1) {
			writes = append(writes, discoveredWrite{Source: source, Method: strings.ToUpper(string(match[1])), APIPath: string(match[2])})
		}
		for _, match := range fetchPattern.FindAllSubmatch(contents, -1) {
			writes = append(writes, discoveredWrite{Source: source, Method: strings.ToUpper(string(match[2])), APIPath: string(match[1])})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("discover web authorization surfaces: %v", err)
	}
	return pages, writes
}

func assertWriteRoleMatchesOpenAPI(t *testing.T, doc *openapi3.T, write PageWrite) {
	t.Helper()
	item := doc.Paths.Find(write.APIPath)
	if item == nil {
		t.Fatalf("A10 write %s %s has no OpenAPI path", write.Method, write.APIPath)
	}
	operation := item.Operations()[strings.ToUpper(write.Method)]
	if operation == nil {
		t.Fatalf("A10 write %s %s has no OpenAPI operation", write.Method, write.APIPath)
	}
	requiredRole, ok := operation.Extensions["x-required-role"].(string)
	if !ok {
		t.Fatalf("A10 write %s %s has no x-required-role", write.Method, write.APIPath)
	}
	want := decisionForRequiredRole(requiredRole)
	if write.Allowed != want {
		t.Errorf("A10 write %s %s decision = %+v, want %+v for %s", write.Method, write.APIPath, write.Allowed, want, requiredRole)
	}
}

func decisionForRequiredRole(role string) RoleDecision {
	switch role {
	case "READONLY":
		return RoleDecision{Readonly: true, AlertAdmin: true, PlatformAdmin: true}
	case "ALERT_ADMIN":
		return RoleDecision{AlertAdmin: true, PlatformAdmin: true}
	case "PLATFORM_ADMIN":
		return RoleDecision{PlatformAdmin: true}
	default:
		return RoleDecision{}
	}
}

func writeExistsInSources(writes []discoveredWrite, sources []string, method, apiPath string) bool {
	for _, write := range writes {
		if contains(sources, write.Source) && write.Method == strings.ToUpper(method) && write.APIPath == apiPath {
			return true
		}
	}
	return false
}

func writeKey(page, method, apiPath string) string {
	return page + " " + strings.ToUpper(method) + " " + apiPath
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test filename")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func TestA12AttributableWritesAreComplete(t *testing.T) {
	want := map[string][]string{
		"login_success":                    {"platform_event.actor_id"},
		"login_failure":                    {"platform_event.actor_subject"},
		"user_create":                      {"app_user.created_by", "platform_event.actor_id"},
		"user_status_change":               {"app_user.enabled_updated_by", "platform_event.actor_id"},
		"user_role_change":                 {"app_user.role_updated_by", "platform_event.actor_id"},
		"user_password_reset":              {"platform_event.actor_id"},
		"instance_create":                  {"instance.created_by"},
		"instance_credential_update":       {"instance.credential_updated_by", "platform_event.actor_id"},
		"instance_remove":                  {"platform_event.actor_id"},
		"alert_rule_or_disposition_change": {"alert_rule.created_by", "alert_rule.updated_by", "alert_rule.enabled_updated_by", "alert_event.actor_id"},
		"collection_task_interval_change":  {"collection_task_config.updated_by"},
		"collection_pause_or_key_rotation": {"instance_collection_config.collection_pause_updated_by", "alert_event.actor_id", "platform_event.actor_subject"},
	}

	seen := make(map[string]bool, len(AttributableWrites))
	for _, operation := range AttributableWrites {
		if seen[operation.ID] {
			t.Errorf("A12 contains duplicate operation %q", operation.ID)
		}
		seen[operation.ID] = true
		if operation.Description == "" {
			t.Errorf("A12 operation %q has no description", operation.ID)
		}
		if len(operation.ActorLocations) == 0 {
			t.Errorf("A12 operation %q has no actor location", operation.ID)
		}
		for _, location := range operation.ActorLocations {
			if location == "" {
				t.Errorf("A12 operation %q has an empty actor location", operation.ID)
			}
		}
		gotLocations := append([]string(nil), operation.ActorLocations...)
		wantLocations := append([]string(nil), want[operation.ID]...)
		sort.Strings(gotLocations)
		sort.Strings(wantLocations)
		if !reflect.DeepEqual(gotLocations, wantLocations) {
			t.Errorf("A12 operation %q actor locations = %v, want %v", operation.ID, gotLocations, wantLocations)
		}
	}

	got := make([]string, 0, len(seen))
	for id := range seen {
		got = append(got, id)
	}
	sort.Strings(got)
	wantIDs := make([]string, 0, len(want))
	for id := range want {
		wantIDs = append(wantIDs, id)
	}
	sort.Strings(wantIDs)
	if !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("A12 operation IDs = %v, want %v", got, wantIDs)
	}
}
