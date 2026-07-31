package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostQueueBillingEligibilityCoversUserSlotCallers(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	dir := filepath.Dir(currentFile)
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	require.NoError(t, err)

	acquireCalls := map[string]struct{}{
		"AcquireUserSlotWithWait":  {},
		"acquireResponsesUserSlot": {},
		"acquireUserSlot":          {},
	}
	wrapperFunctions := map[string]struct{}{
		"AcquireUserSlotWithWait":  {},
		"acquireResponsesUserSlot": {},
		"acquireUserSlot":          {},
	}

	fset := token.NewFileSet()
	covered := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, parseErr, path)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if _, wrapper := wrapperFunctions[fn.Name.Name]; wrapper {
				continue
			}

			var acquirePositions []token.Pos
			var eligibilityPositions []token.Pos
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := calledFunctionName(call.Fun)
				if _, isAcquire := acquireCalls[name]; isAcquire {
					acquirePositions = append(acquirePositions, call.Pos())
				}
				if name == "resolveLatestBillingEligibility" {
					eligibilityPositions = append(eligibilityPositions, call.Pos())
				}
				return true
			})

			for _, acquirePos := range acquirePositions {
				covered++
				foundAfterAcquire := false
				for _, eligibilityPos := range eligibilityPositions {
					if eligibilityPos > acquirePos {
						foundAfterAcquire = true
						break
					}
				}
				require.Truef(t, foundAfterAcquire,
					"%s.%s acquires a user slot without a later resolveLatestBillingEligibility call",
					filepath.Base(path), fn.Name.Name)
			}
		}
	}

	require.Greater(t, covered, 0, "expected to find user-slot acquisition call sites")
}

func calledFunctionName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		return value.Sel.Name
	default:
		return ""
	}
}
