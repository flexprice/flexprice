package codelint

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

const codeComment = "CL001"

// Past this a comment is carrying background, not a constraint.
const maxCommentLines = 4

var (
	directivePattern       = regexp.MustCompile(`^(?:go:|lint:|nolint|want |export |sys |line |extern |\+build)`)
	dividerPattern         = regexp.MustCompile(`^[-=*#~_/+]{3,}|[-=*#~_]{3,}\s*$`)
	taskMarkerPattern      = regexp.MustCompile(`\b(?:TODO|FIXME|XXX|HACK)\b`)
	taskLinkPattern        = regexp.MustCompile(`FLE-\d+|#\d+|https?://`)
	ticketPattern          = regexp.MustCompile(`\bFLE-\d+\b`)
	quarantinePattern      = regexp.MustCompile(`^quarantine:`)
	licensePattern         = regexp.MustCompile(`(?i)@license|\bcopyright\b|SPDX-License-Identifier`)
	commentedCodePattern   = regexp.MustCompile(`^(?:(?:if|for|switch|select)\b.*\{|(?:return|defer|go)\b.*|[\w.\[\]]+\(.*\)|[\w.\[\], ]+\s*:?=\s*.+|\}(?:\s*else\s*\{)?|[\w.]+\+\+|[\w.]+--)$`)
	codePunctuationPattern = regexp.MustCompile(`[(){}]|:=|\+\+|--`)
	wordPattern            = regexp.MustCompile(`[a-z0-9]+`)
	camelBoundaryPattern   = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

// fillerWords carry no meaning when deciding whether a comment only repeats a name.
var fillerWords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "for": true, "to": true, "in": true, "on": true, "at": true,
	"by": true, "with": true, "from": true, "and": true, "or": true, "is": true, "are": true, "be": true,
	"this": true, "that": true, "it": true, "its": true, "as": true, "get": true, "set": true, "return": true,
	"create": true, "new": true, "current": true, "value": true, "given": true, "whether": true,
}

// checkComments rejects comments that repeat the code, carry background
// instead of a constraint, divide sections, or defer work without a link.
func (c *checker) checkComments() {
	for _, file := range c.pass.Files {
		if isGenerated(file) {
			continue
		}
		declaredAt := declarationsByLine(c.pass.Fset, file)
		for _, group := range file.Comments {
			if problem := c.commentProblem(group, declaredAt); problem != "" {
				c.report(group.End()-1, codeComment, "%s", problem)
			}
		}
	}
}

func (c *checker) commentProblem(group *ast.CommentGroup, declaredAt map[int]string) string {
	lines := commentLines(group)
	if len(lines) == 0 {
		return ""
	}
	text := strings.Join(lines, "\n")
	isTask := taskMarkerPattern.MatchString(text)

	for _, line := range lines {
		if dividerPattern.MatchString(line) {
			return "drop the section divider; split the file or let the structure show the grouping"
		}
	}
	if isTask && !taskLinkPattern.MatchString(text) {
		return "link the FLE ticket or issue that tracks this, or do it now"
	}
	if !isTask && !quarantinePattern.MatchString(lines[0]) && ticketPattern.MatchString(text) {
		return "drop the ticket reference; a comment is self-sufficient, say why here"
	}
	if allMatch(lines, commentedCodePattern) && codePunctuationPattern.MatchString(text) {
		return "delete commented-out code; git keeps the history"
	}
	if name, ok := declaredAt[c.pass.Fset.Position(group.End()).Line+1]; ok && restatesName(lines, name) {
		return "drop the comment; it only repeats the name below it. Say why, or say nothing"
	}
	if len(lines) > maxCommentLines && !licensePattern.MatchString(text) {
		return "cut this comment to " + strconv.Itoa(maxCommentLines) + " lines or fewer: keep the constraint, drop the background"
	}
	return ""
}

// commentLines returns the non-empty prose lines of a group with comment
// markers stripped; tool directives such as //go:generate are not prose.
func commentLines(group *ast.CommentGroup) []string {
	var lines []string
	for _, comment := range group.List {
		text := comment.Text
		if strings.HasPrefix(text, "/*") {
			text = strings.TrimSuffix(strings.TrimPrefix(text, "/*"), "*/")
		} else {
			text = strings.TrimPrefix(text, "//")
		}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
			if line != "" && !directivePattern.MatchString(line) {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

// declarationsByLine maps the line a declaration starts on to its name.
func declarationsByLine(fset *token.FileSet, file *ast.File) map[int]string {
	names := map[int]string{}
	record := func(pos token.Pos, ident *ast.Ident) {
		if ident != nil {
			names[fset.Position(pos).Line] = ident.Name
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			record(node.Pos(), node.Name)
		case *ast.TypeSpec:
			record(node.Pos(), node.Name)
		case *ast.ValueSpec:
			if len(node.Names) > 0 {
				record(node.Pos(), node.Names[0])
			}
		case *ast.Field:
			if len(node.Names) > 0 {
				record(node.Pos(), node.Names[0])
			}
		}
		return true
	})
	return names
}

// restatesName is true when every meaningful word of a short comment is
// already in the identifier it documents.
func restatesName(lines []string, name string) bool {
	content := meaningfulWords(strings.Join(lines, " "))
	if len(content) == 0 || len(content) > 6 {
		return false
	}
	nameWords := map[string]bool{}
	for _, word := range meaningfulWords(name) {
		nameWords[word] = true
	}
	for _, word := range content {
		if !nameWords[word] {
			return false
		}
	}
	return true
}

// meaningfulWords splits camelCase, lowercases, singularises and drops filler.
func meaningfulWords(text string) []string {
	text = camelBoundaryPattern.ReplaceAllString(text, "$1 $2")
	var words []string
	for _, word := range wordPattern.FindAllString(strings.ToLower(text), -1) {
		word = singular(word)
		if !fillerWords[word] {
			words = append(words, word)
		}
	}
	return words
}

func singular(word string) string {
	if len(word) > 3 && strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") {
		return word[:len(word)-1]
	}
	return word
}

func allMatch(lines []string, pattern *regexp.Regexp) bool {
	for _, line := range lines {
		if !pattern.MatchString(line) {
			return false
		}
	}
	return true
}
