package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var packageLayers = map[string]int{
	"cmd/acceptance-report":     3,
	"cmd/monitor-agent":         3,
	"cmd/monitor-server":        3,
	"cmd/pg-range-evidence":     3,
	"internal/agent":            2,
	"internal/acceptancereport": 1,
	"internal/alerting":         1,
	"internal/api":              0,
	"internal/capability":       2,
	"internal/clock":            0,
	"internal/collect":          2,
	"internal/db":               0,
	"internal/dbengine":         0,
	"internal/evaluator":        2,
	"internal/httpapi":          2,
	"internal/instance":         1,
	"internal/metric":           1,
	"internal/notify":           1,
	"internal/pgconn":           0,
	"internal/platformdb":       1,
	"internal/platformevent":    1,
	"internal/platformhealth":   1,
	"internal/securitymodel":    0,
	"test/acceptance":           3,
}

func TestInternalPackageArchitecture(t *testing.T) {
	repoRoot := filepath.Dir(internalRoot(t))
	for _, root := range []struct {
		path   string
		prefix string
	}{
		{path: filepath.Join(repoRoot, "internal"), prefix: "internal"},
		{path: filepath.Join(repoRoot, "cmd"), prefix: "cmd"},
		{path: filepath.Join(repoRoot, "test"), prefix: "test"},
	} {
		entries, err := os.ReadDir(root.path)
		if err != nil {
			t.Fatalf("read %s directory: %v", root.prefix, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			packageName := root.prefix + "/" + entry.Name()
			if root.prefix == "internal" {
				if _, forbidden := map[string]bool{"common": true, "util": true, "utils": true, "shared": true, "base": true, "helper": true}[entry.Name()]; forbidden {
					t.Errorf("forbidden package %s", packageName)
				}
			}
			if _, registered := packageLayers[packageName]; !registered {
				t.Errorf("%s is not registered in arch_test.go", packageName)
			}
		}
	}

	for packageName, layer := range packageLayers {
		path := filepath.Join(repoRoot, filepath.FromSlash(packageName))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("registered package %s does not exist", packageName)
			continue
		}
		checkPackage(t, path, packageName, layer)
	}
}

func checkPackage(t *testing.T, path, packageName string, layer int) {
	t.Helper()
	shortName := filepath.Base(path)
	files, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(path, file.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file.Name(), err)
		}
		for _, imp := range parsed.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			const prefix = "github.com/liumingjian/dbs-monitor/internal/"
			if !strings.HasPrefix(path, prefix) {
				continue
			}
			dependency := "internal/" + strings.Split(strings.TrimPrefix(path, prefix), "/")[0]
			dependencyLayer, ok := packageLayers[dependency]
			if !ok {
				t.Errorf("%s imports unregistered %s", packageName, dependency)
				continue
			}
			if packageName == "internal/collect" && dependency == "internal/capability" {
				continue
			}
			if dependencyLayer >= layer {
				t.Errorf("%s (L%d) imports %s (L%d); dependencies must point down", packageName, layer, dependency, dependencyLayer)
			}
		}
		if shortName == "api" && strings.HasSuffix(file.Name(), ".gen.go") {
			continue
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			typeSpec, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}
			if _, ok := typeSpec.Type.(*ast.InterfaceType); !ok {
				return true
			}
			if shortName == "api" && strings.HasSuffix(file.Name(), ".gen.go") {
				return true
			}
			if typeSpec.Name.Name == "DBTX" {
				switch shortName {
				case "alerting", "httpapi", "instance", "metric", "notify":
					return true
				}
			}
			qualified := shortName + "." + typeSpec.Name.Name
			switch qualified {
			case "db.DBTX", "clock.Clock", "pgconn.Dialer", "collect.Collector", "notify.Channel":
				return true
			default:
				t.Errorf("interface %s is not in the T11 interface whitelist", qualified)
			}
			return true
		})
	}
}

func internalRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return cwd
}
