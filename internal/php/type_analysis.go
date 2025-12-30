package php

import (
	"strconv"
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

func (ctx *analysisContext) collectNamespaceUses(root sitter.Node) map[string]string {
	uses := make(map[string]string)
	if root.IsNull() {
		return uses
	}

	stack := []sitter.Node{root}
	content := ctx.bytes()

	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if node.Type() == "namespace_use_declaration" {
			if typeNode := node.ChildByFieldName("type"); !typeNode.IsNull() {
				continue
			}
			prefix := ""
			for i := uint32(0); i < node.NamedChildCount(); i++ {
				child := node.NamedChild(i)
				switch child.Type() {
				case "namespace_name":
					prefix = normalizeFQN(child.Content(content))
				case "namespace_use_group":
					for j := uint32(0); j < child.NamedChildCount(); j++ {
						if child.NamedChild(j).Type() == "namespace_use_clause" {
							ctx.addUseClause(child.NamedChild(j), prefix, uses)
						}
					}
				case "namespace_use_clause":
					ctx.addUseClause(child, "", uses)
				}
			}
			continue
		}

		for i := uint32(0); i < node.NamedChildCount(); i++ {
			stack = append(stack, node.NamedChild(i))
		}
	}

	return uses
}

func (ctx *analysisContext) addUseClause(clause sitter.Node, prefix string, uses map[string]string) {
	if clause.IsNull() {
		return
	}

	content := ctx.bytes()

	aliasNode := clause.ChildByFieldName("alias")
	alias := ""
	if !aliasNode.IsNull() {
		alias = strings.TrimSpace(aliasNode.Content(content))
	}

	var nameNode sitter.Node
	for i := uint32(0); i < clause.NamedChildCount(); i++ {
		if clause.FieldNameForNamedChild(i) == "alias" {
			continue
		}
		child := clause.NamedChild(i)
		switch child.Type() {
		case "qualified_name", "relative_name", "name":
			nameNode = child
		}
		if !nameNode.IsNull() {
			break
		}
	}

	if nameNode.IsNull() {
		return
	}

	base := strings.TrimSpace(nameNode.Content(content))
	full := base
	if prefix != "" {
		full = prefix + "\\" + strings.TrimLeft(base, "\\")
	}
	full = normalizeFQN(full)
	if full == "" {
		return
	}

	if alias == "" {
		alias = shortName(full)
	}

	lowerAlias := strings.ToLower(alias)
	if lowerAlias != "" {
		uses[lowerAlias] = full
		if alias != lowerAlias {
			uses[alias] = full
		}
	}
	uses[strings.ToLower(full)] = full
}

func (ctx *analysisContext) collectTypeNames(typeNode sitter.Node, uses map[string]string) []string {
	if typeNode.IsNull() {
		return nil
	}

	content := ctx.bytes()
	names := make([]string, 0)
	seen := make(map[string]struct{})
	var collect func(n sitter.Node)
	collect = func(n sitter.Node) {
		if n.IsNull() {
			return
		}
		switch n.Type() {
		case "named_type":
			if resolved := ctx.resolveNamedType(n, uses); resolved != "" {
				key := strings.ToLower(resolved)
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
					names = append(names, resolved)
				}
			}
		case "primitive_type":
			raw := strings.TrimSpace(n.Content(content))
			if raw != "" {
				raw = strings.ToLower(raw)
				if _, ok := seen[raw]; !ok {
					seen[raw] = struct{}{}
					names = append(names, raw)
				}
			}
		case "optional_type", "nullable_type":
			for i := uint32(0); i < n.NamedChildCount(); i++ {
				collect(n.NamedChild(i))
			}
			if _, ok := seen["null"]; !ok {
				seen["null"] = struct{}{}
				names = append(names, "null")
			}
		case "union_type", "intersection_type":
			for i := uint32(0); i < n.NamedChildCount(); i++ {
				collect(n.NamedChild(i))
			}
		case "qualified_name", "relative_name", "name":
			candidate := strings.TrimSpace(n.Content(content))
			if candidate == "" {
				break
			}
			resolved := ctx.resolveRawTypeName(candidate, uses)
			if resolved == "" {
				break
			}
			key := strings.ToLower(resolved)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				names = append(names, resolved)
			}
		default:
			for i := uint32(0); i < n.NamedChildCount(); i++ {
				collect(n.NamedChild(i))
			}
			return
		}
	}
	collect(typeNode)

	return names
}

func (ctx *analysisContext) resolveNamedType(node sitter.Node, uses map[string]string) string {
	var typeNode sitter.Node
	for i := uint32(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		switch child.Type() {
		case "qualified_name", "relative_name", "name":
			typeNode = child
		}
		if !typeNode.IsNull() {
			break
		}
	}

	content := ctx.bytes()
	raw := ""
	if !typeNode.IsNull() {
		raw = typeNode.Content(content)
	} else {
		raw = node.Content(content)
	}

	raw = normalizeFQN(raw)
	if raw == "" {
		return ""
	}

	lowered := strings.ToLower(raw)
	if full, ok := uses[lowered]; ok {
		return full
	}

	shortLower := strings.ToLower(shortName(raw))
	if full, ok := uses[shortLower]; ok {
		return full
	}

	return raw
}

func (ctx *analysisContext) resolveRawTypeName(raw string, uses map[string]string) string {
	raw = normalizeFQN(raw)
	if raw == "" {
		return ""
	}

	lowered := strings.ToLower(raw)
	if full, ok := uses[lowered]; ok {
		return full
	}
	if full, ok := uses[strings.ToLower(shortName(raw))]; ok {
		return full
	}

	return raw
}

func mergeTypeNameLists(existing, additions []string) []string {
	if len(additions) == 0 && len(existing) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(existing)+len(additions))
	result := make([]string, 0, len(existing)+len(additions))
	for _, name := range existing {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, name)
	}
	for _, name := range additions {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, name)
	}
	return result
}

func occurrencesFromTypeNames(names []string, line int) []TypeOccurrence {
	if len(names) == 0 {
		return nil
	}
	occ := make([]TypeOccurrence, 0, len(names))
	for _, typ := range names {
		typ = strings.TrimSpace(typ)
		if typ == "" {
			continue
		}
		occ = append(occ, TypeOccurrence{Type: typ, Line: line})
	}
	return occ
}

func mergeTypeOccurrences(existing, additions []TypeOccurrence) []TypeOccurrence {
	if len(additions) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing))
	for _, occ := range existing {
		key := strings.ToLower(occ.Type) + "#" + strconv.Itoa(occ.Line)
		seen[key] = struct{}{}
	}
	for _, add := range additions {
		// We allow empty type for untyped properties
		key := strings.ToLower(add.Type) + "#" + strconv.Itoa(add.Line)
		if _, ok := seen[key]; ok {
			continue
		}
		existing = append(existing, add)
		seen[key] = struct{}{}
	}
	return existing
}

// TypeNamesFromOccurrences deduplicates the types present in the provided occurrences.
func TypeNamesFromOccurrences(entries []TypeOccurrence) []string {
	if len(entries) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(entries))
	types := make([]string, 0, len(entries))
	for _, occ := range entries {
		key := strings.ToLower(occ.Type)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		types = append(types, occ.Type)
	}
	return types
}

// TypeNamesAtOrBefore collapses the occurrences observed up to the requested line.
func TypeNamesAtOrBefore(entries []TypeOccurrence, line int) []string {
	if len(entries) == 0 {
		return nil
	}
	maxLine := -1
	for _, occ := range entries {
		if line >= 0 && occ.Line > line {
			continue
		}
		if occ.Line > maxLine {
			maxLine = occ.Line
		}
	}
	if maxLine == -1 {
		return nil
	}
	seen := make(map[string]struct{})
	var result []string
	for _, occ := range entries {
		if occ.Line != maxLine {
			continue
		}
		key := strings.ToLower(occ.Type)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, occ.Type)
	}
	return result
}

func normalizeFQN(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\\\", "\\"))
	name = strings.TrimLeft(name, "?\\")
	return name
}

func shortName(qualified string) string {
	if i := lastIndexByte(qualified, '\\'); i >= 0 && i+1 < len(qualified) {
		return qualified[i+1:]
	}
	return qualified
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}
