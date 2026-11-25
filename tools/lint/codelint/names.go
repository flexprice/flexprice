package codelint

import (
	"go/types"
	"strings"
	"unicode"
)

const codeTypeSuffix = "CL007"

// typeSuffixes name the shape of a value instead of what it holds.
var typeSuffixes = []string{"Map", "Slice", "Arr", "Array", "Struct", "Str", "Ptr", "Obj", "Interface"}

// checkNames flags variables, fields and parameters named after their type.
func (c *checker) checkNames() {
	for ident, obj := range c.pass.TypesInfo.Defs {
		if _, ok := obj.(*types.Var); !ok {
			continue
		}
		if suffix := typeSuffix(ident.Name); suffix != "" {
			c.report(ident.Pos(), codeTypeSuffix, "`%s` is named after its type; name it for what it holds, such as `%s`", ident.Name, suggestion(ident.Name, suffix))
		}
	}
}

// typeSuffix returns the offending suffix of a camelCase local name, or "".
func typeSuffix(name string) string {
	if name == "" || !unicode.IsLower(rune(name[0])) {
		return ""
	}
	for _, suffix := range typeSuffixes {
		if !strings.HasSuffix(name, suffix) || len(name) <= len(suffix)+2 {
			continue
		}
		before := rune(name[len(name)-len(suffix)-1])
		if unicode.IsLower(before) || unicode.IsDigit(before) {
			return suffix
		}
	}
	return ""
}

func suggestion(name, suffix string) string {
	base := strings.TrimSuffix(name, suffix)
	switch suffix {
	case "Map":
		return base + "ByID"
	case "Slice", "Arr", "Array":
		if strings.HasSuffix(base, "s") {
			return base
		}
		return base + "s"
	default:
		return base
	}
}
