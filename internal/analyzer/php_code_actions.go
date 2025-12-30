package analyzer

import (
	"fmt"
	"regexp"
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	php "github.com/shinyvision/vimfony/internal/php"
	"github.com/shinyvision/vimfony/internal/utils"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func (a *phpAnalyzer) OnCodeAction(context *glsp.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	a.mu.RLock()
	store := a.docStore
	a.mu.RUnlock()

	if store == nil {
		return nil, nil
	}

	path := utils.UriToPath(string(params.TextDocument.URI))
	doc, err := store.Get(path)
	if err != nil {
		return nil, nil
	}

	// Snapshot of the index
	var index php.IndexedTree
	doc.Read(func(_ *sitter.Tree, _ []byte, idx php.IndexedTree) {
		index = idx
	})

	// Class at cursor position
	cursorLine := int(params.Range.Start.Line) + 1
	var targetClass *php.ClassInfo
	for _, class := range index.Classes {
		if cursorLine >= class.StartLine && cursorLine <= class.EndLine {
			targetClass = &class
			break
		}
	}

	if targetClass == nil {
		return nil, nil
	}

	classProperties := make(map[string]map[string]bool)
	for name, occurrences := range index.Properties {
		for _, occ := range occurrences {
			if occ.Line >= targetClass.StartLine && occ.Line <= targetClass.EndLine {
				// Found property in this class
				if classProperties[name] == nil {
					classProperties[name] = make(map[string]bool)
				}
				if len(occ.Type) > 0 {
					classProperties[name][occ.Type] = true
				}
			}
		}
	}

	if len(classProperties) == 0 {
		return nil, nil
	}

	existingMethods := make(map[string]bool)
	collectMethods := func(funcs []php.FunctionInfo) {
		for _, f := range funcs {
			if f.Range.StartLine >= targetClass.StartLine && f.Range.EndLine <= targetClass.EndLine {
				name := f.Name
				// Might be prefixed with "ClassName::" or "Namespace\ClassName::"
				if idx := strings.LastIndex(name, "::"); idx >= 0 {
					name = name[idx+2:]
				}
				existingMethods[strings.ToLower(name)] = true
			}
		}
	}
	collectMethods(index.PublicFunctions)
	collectMethods(index.ProtectedFunctions)
	collectMethods(index.PrivateFunctions)

	// Primitives set for type check
	primitives := map[string]bool{
		"int": true, "float": true, "string": true, "bool": true,
		"array": true, "iterable": true, "callable": true, "void": true,
		"mixed": true, "object": true, "null": true, "false": true, "true": true,
		"self": true, "parent": true, "static": true,
	}

	shortenType := func(fqn string) string {
		if primitives[strings.ToLower(fqn)] {
			return fqn
		}

		var bestAlias string
		for alias, full := range index.Uses {
			if full == fqn {
				if alias != strings.ToLower(alias) {
					return alias
				}
				bestAlias = alias
			}
		}
		if bestAlias != "" {
			return bestAlias
		}

		// Check current namespace
		if targetClass.Namespace != "" {
			prefix := targetClass.Namespace + "\\"
			if after, ok := strings.CutPrefix(fqn, prefix); ok {
				suffix := after
				if !strings.Contains(suffix, "\\") {
					return suffix
				}
			}
		}

		// Fallback to FQN
		if !strings.HasPrefix(fqn, "\\") {
			return "\\" + fqn
		}
		return fqn
	}

	formatType := func(typeSet map[string]bool) string {
		var types []string
		hasNull := false
		for t := range typeSet {
			if t == "null" {
				hasNull = true
			} else {
				types = append(types, shortenType(t))
			}
		}

		if len(types) == 0 {
			return ""
		}

		if len(types) == 1 {
			t := types[0]
			if hasNull {
				return "?" + t
			}
			return t
		}

		// Multiple types
		res := strings.Join(types, "|")
		if hasNull {
			res += "|null"
		}
		return res
	}

	isGetterMissing := func(name, typeStr string) bool {
		getterName := getGetterName(name, typeStr)
		exists := existingMethods[strings.ToLower(getterName)]

		if !exists && strings.HasPrefix(getterName, "is") && len(getterName) > 2 {
			altName := "get" + getterName[2:]
			if existingMethods[strings.ToLower(altName)] {
				exists = true
			}
		}
		return !exists
	}

	isSetterMissing := func(name string) bool {
		setterName := getSetterName(name)
		return !existingMethods[strings.ToLower(setterName)]
	}

	var propertiesForGetter []string
	var propertiesForSetter []string

	for name, typeSet := range classProperties {
		typeStr := formatType(typeSet)

		if isGetterMissing(name, typeStr) {
			propertiesForGetter = append(propertiesForGetter, name)
		}

		if isSetterMissing(name) {
			propertiesForSetter = append(propertiesForSetter, name)
		}
	}

	var actions []protocol.CodeAction

	generateCode := func(props []string, generateGetter, generateSetter bool) string {
		var parts []string
		for _, name := range props {
			typeStr := formatType(classProperties[name])

			if generateGetter {
				var sb strings.Builder
				sb.WriteString(fmt.Sprintf("    public function %s()", getGetterName(name, typeStr)))
				if typeStr != "" {
					sb.WriteString(fmt.Sprintf(": %s", typeStr))
				} else {
					sb.WriteString(": mixed")
				}
				sb.WriteString("\n    {\n")
				sb.WriteString(fmt.Sprintf("        return $this->%s;\n", name))
				sb.WriteString("    }")
				parts = append(parts, sb.String())
			}

			if generateSetter {
				var sb strings.Builder
				sb.WriteString(fmt.Sprintf("    public function %s(", getSetterName(name)))
				if typeStr != "" {
					sb.WriteString(fmt.Sprintf("%s ", typeStr))
				} else {
					sb.WriteString("mixed ")
				}
				sb.WriteString(fmt.Sprintf("$%s): void\n", name))
				sb.WriteString("    {\n")
				sb.WriteString(fmt.Sprintf("        $this->%s = $%s;\n", name, name))
				sb.WriteString("    }")
				parts = append(parts, sb.String())
			}
		}
		return strings.Join(parts, "\n\n")
	}

	// Default = end of class
	insertionPos := protocol.Position{
		Line:      uint32(targetClass.EndLine - 1),
		Character: 0,
	}

	// Still try to find closest to cursor...
	node, content, _, found := doc.GetNodeAt(params.Range.Start)
	if found {
		for n := node; !n.IsNull(); n = n.Parent() {
			t := n.Type()
			if t == "method_declaration" || t == "property_declaration" || t == "const_declaration" {
				// We're inside of a declaration, move past it..
				insertionPos.Line = uint32(n.EndPoint().Row + 1)
				insertionPos.Character = 0
				break
			}
			if t == "declaration_list" || t == "class_body" {
				// We're in the body.. all good
				insertionPos.Line = params.Range.Start.Line + 1
				insertionPos.Character = 0
				break
			}
			if t == "class_declaration" {
				if params.Range.Start.Line >= uint32(n.StartPoint().Row) && params.Range.Start.Line <= uint32(n.EndPoint().Row) {
					// Last check
					insertionPos.Line = params.Range.Start.Line + 1
					insertionPos.Character = 0
				}
				break
			}
		}
	} else {
		// We can't easily get content without GetNodeAt or Read.
		doc.Read(func(_ *sitter.Tree, c []byte, _ php.IndexedTree) {
			content = append([]byte(nil), c...)
		})
	}

	// We always add newlines (well only if the user didn't add them)
	calculateSpacing := func(pos protocol.Position, content []byte) (string, string) {
		offset := offsetAt(content, pos)
		if offset < 0 || offset > len(content) {
			return "\n\n", "\n\n"
		}

		// Scans backwards for newlines
		newlinesBefore := 0
		for i := offset - 1; i >= 0; i-- {
			b := content[i]
			if b == '\n' {
				newlinesBefore++
				if newlinesBefore >= 2 {
					break // Found one
				}
			} else if b != ' ' && b != '\t' && b != '\r' {
				break // This is content
			}
		}

		// Also scan forwards...
		newlinesAfter := 0
		for i := offset; i < len(content); i++ {
			b := content[i]
			if b == '\n' {
				newlinesAfter++
				if newlinesAfter >= 2 {
					break
				}
			} else if b != ' ' && b != '\t' && b != '\r' {
				break
			}
		}

		prefix := ""
		switch newlinesBefore {
		case 0:
			prefix = "\n\n"
		case 1:
			prefix = "\n"
		}

		suffix := ""
		switch newlinesAfter {
		case 0:
			suffix = "\n\n"
		case 1:
			suffix = "\n"
		}

		return prefix, suffix
	}

	prefix, suffix := calculateSpacing(insertionPos, content)

	// getters & setters
	var bothProps []string
	for name, typeSet := range classProperties {
		typeStr := formatType(typeSet)
		if isGetterMissing(name, typeStr) && isSetterMissing(name) {
			bothProps = append(bothProps, name)
		}
	}

	if len(bothProps) > 0 {
		code := prefix + generateCode(bothProps, true, true) + suffix
		actions = append(actions, createCodeAction("Generate getters & setters", code, params.TextDocument.URI, insertionPos))
	}

	if len(propertiesForGetter) > 0 {
		code := prefix + generateCode(propertiesForGetter, true, false) + suffix
		actions = append(actions, createCodeAction("Generate getters", code, params.TextDocument.URI, insertionPos))
	}

	if len(propertiesForSetter) > 0 {
		code := prefix + generateCode(propertiesForSetter, false, true) + suffix
		actions = append(actions, createCodeAction("Generate setters", code, params.TextDocument.URI, insertionPos))
	}

	return actions, nil
}

// PHPStorm generates with isBooleanProp so I think it's nice to do the same?
func getGetterName(name, typeStr string) string {
	isBool := typeStr == "bool" || typeStr == "?bool"
	if isBool {
		matched, _ := regexp.MatchString(`^is[A-Z]`, name)
		if matched {
			return name
		}
		return "is" + php.ToPascalCase(name)
	}
	return "get" + php.ToPascalCase(name)
}

func getSetterName(name string) string {
	return "set" + php.ToPascalCase(name)
}

func createCodeAction(title, newText, uri string, pos protocol.Position) protocol.CodeAction {
	kind := protocol.CodeActionKindRefactor
	return protocol.CodeAction{
		Title: title,
		Kind:  &kind,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[protocol.DocumentUri][]protocol.TextEdit{
				protocol.DocumentUri(uri): {
					{
						Range: protocol.Range{
							Start: pos,
							End:   pos,
						},
						NewText: newText,
					},
				},
			},
		},
	}
}

func offsetAt(content []byte, pos protocol.Position) int {
	line := int(pos.Line)
	character := int(pos.Character)

	currentLine := 0
	offset := 0

	// Find line
	for offset < len(content) && currentLine < line {
		if content[offset] == '\n' {
			currentLine++
		}
		offset++
	}

	if currentLine != line {
		return -1
	}

	// Find char
	col := 0
	for offset < len(content) && content[offset] != '\n' && col < character {
		offset++
		col++
	}

	return offset
}
