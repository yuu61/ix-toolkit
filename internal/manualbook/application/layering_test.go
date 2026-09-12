package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLayerDependenciesAndProcessOwnership(t *testing.T) {
	const prefix = "github.com/yuu61/ix-toolkit/internal/manualbook/"
	allowed := map[string]map[string]bool{
		"domain":         {},
		"infrastructure": {"domain": true},
		"application":    {"domain": true, "infrastructure": true},
		"cli":            {"application": true},
	}
	for layer, dependencies := range allowed {
		files, err := os.ReadDir(filepath.Join("..", layer))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range files {
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			path := filepath.Join("..", layer, entry.Name())
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			imports := map[string]string{}
			for _, imp := range f.Imports {
				name, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				alias := filepath.Base(name)
				if imp.Name != nil {
					alias = imp.Name.Name
				}
				imports[alias] = name
				if strings.HasPrefix(name, prefix) && !dependencies[strings.TrimPrefix(name, prefix)] {
					t.Errorf("%s: forbidden layer dependency %s", path, name)
				}
				if layer == "domain" && (name == "os" || name == "io" || strings.HasPrefix(name, "io/") || name == "net" || strings.HasPrefix(name, "net/") || name == "syscall" || name == "unsafe" || name == "path/filepath" || name == "os/exec" || strings.Contains(strings.Split(name, "/")[0], ".")) {
					t.Errorf("%s: domain depends on external I/O or technology: %s", path, name)
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				s, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := s.X.(*ast.Ident)
				if !ok {
					return true
				}
				pkg, member := imports[id.Name], s.Sel.Name
				if layer != "cli" && pkg == "os" && member == "Exit" {
					t.Errorf("%s: process termination belongs to cli", fset.Position(s.Pos()))
				}
				if layer == "infrastructure" && ((pkg == "os" && (member == "Stdout" || member == "Stderr")) || (pkg == "fmt" && (member == "Print" || member == "Printf" || member == "Println"))) {
					t.Errorf("%s: infrastructure must return results or use an injected progress sink", fset.Position(s.Pos()))
				}
				return true
			})
		}
	}
}
