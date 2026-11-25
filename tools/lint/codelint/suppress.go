package codelint

import (
	"go/token"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const codeSuppression = "CL000"

var ignorePattern = regexp.MustCompile(`^lint:ignore ((?:CL\d{3})(?:,CL\d{3})*)\s*(.*)$`)

// ignores maps file and line to the codes a //lint:ignore directive silences there.
type ignores map[string]map[int]map[string]bool

// collectIgnores reads every //lint:ignore directive and reports the ones
// without a reason, since the directive itself explains nothing.
func collectIgnores(pass *analysis.Pass) ignores {
	result := ignores{}
	for _, file := range pass.Files {
		for _, group := range file.Comments {
			for _, comment := range group.List {
				match := ignorePattern.FindStringSubmatch(directiveText(comment.Text))
				if match == nil {
					continue
				}
				if strings.TrimSpace(match[2]) == "" {
					pass.Reportf(comment.Pos(), "%s: give the reason after the code: //lint:ignore %s <why this is right here>", codeSuppression, match[1])
					continue
				}
				position := pass.Fset.Position(comment.Pos())
				result.add(position.Filename, position.Line, strings.Split(match[1], ","))
			}
		}
	}
	return result
}

// directiveText strips the comment markers from either comment form.
func directiveText(comment string) string {
	if strings.HasPrefix(comment, "/*") {
		comment = strings.TrimSuffix(strings.TrimPrefix(comment, "/*"), "*/")
	}
	return strings.TrimSpace(strings.TrimPrefix(comment, "//"))
}

func (ig ignores) add(filename string, line int, codes []string) {
	if ig[filename] == nil {
		ig[filename] = map[int]map[string]bool{}
	}
	if ig[filename][line] == nil {
		ig[filename][line] = map[string]bool{}
	}
	for _, code := range codes {
		ig[filename][line][code] = true
	}
}

// covers is true when the directive sits on the finding's line or the line above.
func (ig ignores) covers(position token.Position, code string) bool {
	lines := ig[position.Filename]
	return lines[position.Line][code] || lines[position.Line-1][code]
}
