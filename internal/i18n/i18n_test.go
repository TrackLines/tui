package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestLanguageChoice(t *testing.T) {
	defer Set("en")
	for _, tc := range []struct{ setting, lcAll, lang, want string }{
		{"nl", "", "en_GB.UTF-8", "Opgeslagen."},
		{"en", "", "nl_NL.UTF-8", "Saved."},
		{"system", "", "nl_NL.UTF-8", "Opgeslagen."},
		{"system", "nl_BE", "en_GB.UTF-8", "Opgeslagen."}, // LC_ALL wins
		{"system", "", "de_DE.UTF-8", "Saved."},           // no German: English
		{"system", "", "C", "Saved."},
		{"system", "", "", "Saved."},
		{"klingon", "", "nl_NL.UTF-8", "Saved."}, // config.Load already refuses it; English anyway
	} {
		t.Setenv("LC_ALL", tc.lcAll)
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", tc.lang)
		Set(tc.setting)
		if got := T("Saved."); got != tc.want {
			t.Fatalf("%+v: %q", tc, got)
		}
	}
	Set("nl")
	if got := F("Moved to %s.", "Done"); got != "Verplaatst naar Done." { // names from the API stay as they are
		t.Fatalf("F: %q", got)
	}
}

// sources are the files whose text people read.
var sources = []string{"../ui/*.go", "../../cmd/tracklines/*.go", "../update/*.go"}

// prose looks like words for people: two words, or a capitalised word.
var prose = regexp.MustCompile(`[A-Za-z]{2,}[ ,.:;’'/–-]+[A-Za-z]{2,}|^[A-Z][a-z]{2,}`)

// notText are literals that look like prose but aren't shown, or are names that stay as they are.
var notText = map[string]bool{
	"TrackLin.es": true, "Bugfixes": true, "Claude": true,
	"You":                         true, // the API's name for the viewer, compared, then translated
	"Accept":                      true, // HTTP
	"application/vnd.github+json": true,
	".tracklines-update-*":        true, // a temp file name
	"tui stopped: %v":             true, // a report to Bugfixes, for us
	"running:refinement:":         true, // fetch tags
	"running:standup:":            true,
	"xdg-open":                    true, // a command
}

// Every T and F text has a Dutch translation, and nothing that reads as prose is shown without
// going through them: nothing stays English-only in Dutch by accident.
func TestEveryTextIsTranslated(t *testing.T) {
	for _, glob := range sources {
		files, _ := filepath.Glob(glob)
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			translated := map[*ast.BasicLit]bool{}
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.ImportSpec:
					translated[n.Path] = true
				case *ast.Field:
					if n.Tag != nil {
						translated[n.Tag] = true
					}
				case *ast.CallExpr:
					name := ""
					switch fn := n.Fun.(type) {
					case *ast.Ident:
						name = fn.Name
					case *ast.SelectorExpr:
						name = fn.Sel.Name
					}
					if (name == "T" || name == "F") && len(n.Args) > 0 {
						if lit, ok := n.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							translated[lit] = true
							s, _ := strconv.Unquote(lit.Value)
							if _, ok := nl[s]; !ok {
								t.Errorf("%s: no Dutch for %q", fset.Position(lit.Pos()), s)
							}
						}
					}
				}
				return true
			})
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || translated[lit] {
					return true
				}
				s, _ := strconv.Unquote(lit.Value)
				if prose.MatchString(s) && !notText[s] && !strings.HasPrefix(s, "https://") {
					t.Errorf("%s: shown untranslated: %q", fset.Position(lit.Pos()), s)
				}
				return true
			})
		}
	}
}
