package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var packageLayers = map[string]int{
	"agent":     2,
	"alerting":  1,
	"api":       0,
	"clock":     0,
	"collect":   2,
	"db":        0,
	"evaluator": 2,
	"httpapi":   2,
	"instance":  1,
	"metric":    1,
	"pgconn":    0,
}

func TestInternalPackageArchitecture(t *testing.T) {
	root := internalRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read internal directory: %v", err)
	}

	var found []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		found = append(found, name)
		if _, forbidden := map[string]bool{"common": true, "util": true, "utils": true, "shared": true, "base": true, "helper": true}[name]; forbidden {
			t.Errorf("forbidden package internal/%s; see docs/design/05-backend-code-structure.md §3.3", name)
		}
		if _, registered := packageLayers[name]; !registered {
			t.Errorf("internal/%s is not registered in arch_test.go", name)
		}
	}
	sort.Strings(found)

	for name, layer := range packageLayers {
		path := filepath.Join(root, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("registered package internal/%s does not exist", name)
			continue
		}
		checkPackage(t, path, name, layer)
	}
}

func checkPackage(t *testing.T, path, packageName string, layer int) {
	t.Helper()
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
			dependency := strings.Split(strings.TrimPrefix(path, prefix), "/")[0]
			dependencyLayer, ok := packageLayers[dependency]
			if !ok {
				t.Errorf("internal/%s imports unregistered internal/%s", packageName, dependency)
				continue
			}
			if dependencyLayer >= layer {
				t.Errorf("internal/%s (L%d) imports internal/%s (L%d); dependencies must point down", packageName, layer, dependency, dependencyLayer)
			}
		}
		if packageName == "api" && strings.HasSuffix(file.Name(), ".gen.go") {
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
			if packageName == "api" && strings.HasSuffix(file.Name(), ".gen.go") {
				return true
			}
			if (packageName == "alerting" || packageName == "httpapi" || packageName == "instance" || packageName == "metric") && typeSpec.Name.Name == "DBTX" {
				return true
			}
			qualified := packageName + "." + typeSpec.Name.Name
			if qualified != "db.DBTX" && qualified != "clock.Clock" && qualified != "pgconn.Dialer" && qualified != "collect.Collector" {
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
