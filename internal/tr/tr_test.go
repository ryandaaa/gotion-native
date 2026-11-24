package tr

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// sources are the Go files whose texts people read.
func sources(t *testing.T) []*ast.File {
	var out []*ast.File
	fset := token.NewFileSet()
	for _, dir := range []string{"../app", "../doc", "../syncer", "../notion"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, f, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, file)
		}
	}
	return out
}

func literal(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

func callName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return callName(x.X) + "." + x.Sel.Name
	case *ast.CallExpr:
		return callName(x.Fun)
	}
	return ""
}

// Every text said with T or marked with N has its English.
func TestEveryTextHasEnglish(t *testing.T) {
	n := 0
	for _, f := range sources(t) {
		ast.Inspect(f, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			if name := callName(call.Fun); name != "tr.T" && name != "tr.N" {
				return true
			}
			if s, ok := literal(call.Args[0]); ok {
				n++
				if !Has(s) {
					t.Errorf("no English for %q", s)
				}
			}
			return true
		})
	}
	if n < 300 {
		t.Errorf("only %d texts found: is the scan reading the sources?", n)
	}
}

// Texts handed straight to the screen go through T: a literal with words
// given to Text, Label, Placeholder, Tooltip, toast, a button or a menu
// item is a text someone forgot.
func TestNoTextSkipsT(t *testing.T) {
	shown := map[string]int{ // the argument that is shown
		"ui.Text": 1, "Label": 0, "Placeholder": 0, "Tooltip": 0, "a.toast": 0, "a.btn": 1,
		"m.Item": 0, "ui.Checkbox": 2, "ui.PrimaryButton": 1, "ui.Button": 1, "ui.Link": 1,
	}
	same := map[string]bool{"Database": true, "Template": true, "Token": true, "URL": true, "Mode": true,
		"Workspace": true, "Filter": true, "Email": true, "Status": true, "Markdown": true, "Link": true, "Zoom": true, "gotion": true, "Bahasa · Language": true}
	for _, f := range sources(t) {
		ast.Inspect(f, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := callName(call.Fun)
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if _, isMethod := shown[sel.Sel.Name]; isMethod && !strings.Contains(name, ".") {
					name = sel.Sel.Name
				}
				if _, ok := shown[name]; !ok {
					name = sel.Sel.Name
				}
			}
			i, ok := shown[name]
			if !ok || i >= len(call.Args) {
				return true
			}
			s, ok := literal(call.Args[i])
			if !ok || same[s] || strings.HasPrefix(s, "http") {
				return true
			}
			words := 0
			for _, r := range s {
				if unicode.IsLetter(r) {
					words++
				}
			}
			if words >= 3 && strings.ContainsAny(s, "aeiou") && s != strings.ToUpper(s) {
				t.Errorf("%s(%q) is shown without tr.T", name, s)
			}
			return true
		})
	}
}

func TestPick(t *testing.T) {
	for in, want := range map[string]string{"id-ID": ID, "in": ID, "en-GB": EN, "de": EN, "": EN} {
		if got := Pick([]string{in}); got != want {
			t.Errorf("Pick(%q) = %s", in, got)
		}
	}
	if Pick([]string{"de-DE", "id"}) != ID {
		t.Error("a later preferred language is not looked at")
	}
	Set(EN)
	if T("Batal") != "Cancel" || T("not a text") != "not a text" {
		t.Error("English")
	}
	Set(ID)
	if T("Batal") != "Batal" {
		t.Error("Indonesian")
	}
}
