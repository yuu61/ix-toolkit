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

const (
	domainLayer         = "domain"
	infrastructureLayer = "infrastructure"
)

func TestLayerDependenciesAndProcessOwnership(t *testing.T) {
	allowed := map[string]map[string]bool{
		domainLayer:         {},
		infrastructureLayer: {domainLayer: true},
		"application":       {domainLayer: true, infrastructureLayer: true},
		"cli":               {"application": true},
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
			checkLayerFile(t, layer, dependencies, entry.Name())
		}
	}
}

func checkLayerFile(t *testing.T, layer string, dependencies map[string]bool, name string) {
	const prefix = "github.com/yuu61/ix-toolkit/internal/manualbook/"
	path := filepath.Join("..", layer, name)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	imports := map[string]string{}
	for _, imp := range f.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		alias := filepath.Base(importPath)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		imports[alias] = importPath
		if strings.HasPrefix(importPath, prefix) && !dependencies[strings.TrimPrefix(importPath, prefix)] {
			t.Errorf("%s: forbidden layer dependency %s", path, importPath)
		}
		if layer == domainLayer && externalDependency(importPath) {
			t.Errorf("%s: domain depends on external I/O or technology: %s", path, importPath)
		}
	}
	checkProcessOwnership(t, layer, f, fset, imports)
}

func checkProcessOwnership(t *testing.T, layer string, f *ast.File, fset *token.FileSet, imports map[string]string) {
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
		if layer == infrastructureLayer && usesProcessOutput(pkg, member) {
			t.Errorf("%s: infrastructure must return results or use an injected progress sink", fset.Position(s.Pos()))
		}
		return true
	})
}

func externalDependency(name string) bool {
	switch name {
	case "os", "io", "net", "syscall", "unsafe", "path/filepath", "os/exec":
		return true
	}
	return strings.HasPrefix(name, "io/") || strings.HasPrefix(name, "net/") || strings.Contains(strings.Split(name, "/")[0], ".")
}

func usesProcessOutput(pkg, member string) bool {
	return (pkg == "os" && (member == "Stdout" || member == "Stderr")) || (pkg == "fmt" && (member == "Print" || member == "Printf" || member == "Println"))
}
