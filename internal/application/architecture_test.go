package application

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Check production dependencies, including files that might be added in a later session.
func TestArchitectureDependencies(t *testing.T) {
	allowed := map[string]map[string]bool{
		"core":          {},
		"application":   {"core": true},
		"api":           {"core": true},
		"storage":       {"core": true},
		"destination":   {"core": true},
		"observability": {"core": true},
	}
	for pkg, deps := range allowed {
		entries, err := os.ReadDir(filepath.Join("..", pkg))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			path := filepath.Join("..", pkg, entry.Name())
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				name, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(name, "hookrelay/internal/") {
					dep := strings.TrimPrefix(name, "hookrelay/internal/")
					if !deps[dep] {
						t.Errorf("%s imports forbidden package %s", path, name)
					}
				}
				if pkg == "core" || pkg == "application" {
					if strings.Contains(strings.Split(name, "/")[0], ".") || name == "net" || strings.HasPrefix(name, "net/") || name == "database/sql" || strings.HasPrefix(name, "database/sql/") || name == "os" || strings.HasPrefix(name, "os/") {
						t.Errorf("%s imports infrastructure %s", path, name)
					}
				}
			}
		}
	}
}
