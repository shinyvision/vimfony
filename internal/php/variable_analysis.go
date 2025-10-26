package php

import (
	"fmt"
	"regexp"
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

var docblockVarRe = regexp.MustCompile(`@var\s+([^\s]+)\s+\$([A-Za-z_][A-Za-z0-9_]*)`)

func (ctx *analysisContext) collectFunctionVariableTypes(properties map[string][]TypeOccurrence) map[string]FunctionScope {
	result := make(map[string]FunctionScope)
	root := ctx.rootNode()
	if root.IsNull() {
		return result
	}

	uses := ctx.collectNamespaceUses(root)
	stack := []sitter.Node{root}

	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		switch node.Type() {
		case "method_declaration", "function_definition", "function_declaration":
			funcName := ctx.functionIdentifier(node)
			scope := FunctionScope{
				Variables: ctx.collectVariableTypesForFunction(node, uses, properties),
			}
			result[funcName] = scope
		}

		for i := uint32(0); i < node.NamedChildCount(); i++ {
			stack = append(stack, node.NamedChild(i))
		}
	}

	return result
}

func (ctx *analysisContext) collectVariableTypesForFunction(node sitter.Node, uses map[string]string, properties map[string][]TypeOccurrence) map[string][]TypeOccurrence {
	types := make(map[string][]TypeOccurrence)
	content := ctx.bytes()

	params := node.ChildByFieldName("parameters")
	if !params.IsNull() {
		for i := uint32(0); i < params.NamedChildCount(); i++ {
			param := params.NamedChild(i)
			nameNode := param.ChildByFieldName("name")
			name := VariableNameFromNode(nameNode, content)
			if name == "" {
				continue
			}
			typeNames := ctx.collectTypeNames(param.ChildByFieldName("type"), uses)
			if len(typeNames) == 0 {
				continue
			}
			line := int(param.StartPoint().Row) + 1
			occ := occurrencesFromTypeNames(typeNames, line)
			types[name] = mergeTypeOccurrences(types[name], occ)
		}
	}

	body := ctx.functionBodyNode(node)
	if body.IsNull() {
		return types
	}

	pendingDoc := make(map[string][]string)
	for i := uint32(0); i < body.NamedChildCount(); i++ {
		stmt := body.NamedChild(i)
		switch stmt.Type() {
		case "comment":
			varName, docTypes := ctx.parseDocblockVar(stmt, uses)
			if varName != "" && len(docTypes) > 0 {
				pendingDoc[varName] = docTypes
			}
			continue
		case "expression_statement":
			expr := stmt.NamedChild(0)
			if expr.IsNull() || expr.Type() != "assignment_expression" {
				pendingDoc = make(map[string][]string)
				continue
			}
			left := expr.ChildByFieldName("left")
			varName := VariableNameFromNode(left, content)
			if varName == "" {
				pendingDoc = make(map[string][]string)
				continue
			}
			line := int(expr.StartPoint().Row) + 1
			right := expr.ChildByFieldName("right")
			inferred := ctx.inferExpressionTypeNames(right, uses, types, properties, line-1)
			docs := pendingDoc[varName]
			combined := mergeTypeNameLists(docs, inferred)
			if len(combined) > 0 {
				occ := occurrencesFromTypeNames(combined, line)
				types[varName] = mergeTypeOccurrences(types[varName], occ)
			}
			delete(pendingDoc, varName)
		default:
			pendingDoc = make(map[string][]string)
			continue
		}
		pendingDoc = make(map[string][]string)
	}

	return types
}

func (ctx *analysisContext) functionBodyNode(node sitter.Node) sitter.Node {
	if body := node.ChildByFieldName("body"); !body.IsNull() {
		return body
	}
	return sitter.Node{}
}

func (ctx *analysisContext) functionIdentifier(node sitter.Node) string {
	name := ctx.functionNameFromNode(node)
	if name != "" {
		return name
	}
	return fmt.Sprintf("anonymous@%d", int(node.StartPoint().Row)+1)
}

func (ctx *analysisContext) functionNameFromNode(node sitter.Node) string {
	content := ctx.bytes()
	nameNode := node.ChildByFieldName("name")
	if nameNode.IsNull() {
		return ""
	}
	return strings.TrimSpace(nameNode.Content(content))
}

func (ctx *analysisContext) parseDocblockVar(node sitter.Node, uses map[string]string) (string, []string) {
	content := ctx.bytes()
	text := node.Content(content)
	matches := docblockVarRe.FindStringSubmatch(text)
	if len(matches) < 3 {
		return "", nil
	}
	typeExpr := matches[1]
	varName := matches[2]
	parts := strings.Split(typeExpr, "|")
	types := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		nullHint := false
		if strings.HasPrefix(part, "?") {
			nullHint = true
			part = strings.TrimPrefix(part, "?")
		}
		if nullHint {
			types = mergeTypeNameLists(types, []string{"null"})
		}
		lower := strings.ToLower(part)
		if lower == "null" {
			types = mergeTypeNameLists(types, []string{"null"})
			continue
		}
		if strings.HasSuffix(part, "[]") {
			types = mergeTypeNameLists(types, []string{"array"})
			continue
		}
		resolved := ctx.resolveRawTypeName(part, uses)
		if resolved == "" {
			resolved = part
		}
		types = mergeTypeNameLists(types, []string{resolved})
	}
	return varName, types
}

func (ctx *analysisContext) inferExpressionTypeNames(expr sitter.Node, uses map[string]string, current map[string][]TypeOccurrence, properties map[string][]TypeOccurrence, line int) []string {
	if expr.IsNull() {
		return nil
	}
	content := ctx.bytes()

	switch expr.Type() {
	case "member_access_expression", "nullsafe_member_access_expression":
		if name := memberAccessPropertyName(expr, content); name != "" {
			return TypeNamesFromOccurrences(properties[name])
		}
		object := expr.ChildByFieldName("object")
		if object.IsNull() {
			return nil
		}
		if object.Type() == "variable_name" {
			varName := VariableNameFromNode(object, content)
			if varName != "" {
				return TypeNamesAtOrBefore(current[varName], line)
			}
		}
	case "variable_name":
		varName := VariableNameFromNode(expr, content)
		if varName == "" {
			return nil
		}
		return TypeNamesAtOrBefore(current[varName], line)
	case "qualified_name", "relative_name", "name":
		candidate := strings.TrimSpace(expr.Content(content))
		if candidate == "" {
			return nil
		}
		resolved := ctx.resolveRawTypeName(candidate, uses)
		if resolved == "" {
			resolved = candidate
		}
		return []string{resolved}
	case "string":
		return []string{"string"}
	case "integer":
		return []string{"int"}
	case "float", "floating_point_number", "floating_literal":
		return []string{"float"}
	case "true", "false", "boolean":
		return []string{"bool"}
	case "null", "null_literal":
		return []string{"null"}
	case "array_creation_expression":
		return []string{"array"}
	case "object_creation_expression":
		return ctx.collectTypeNames(expr.ChildByFieldName("type"), uses)
	case "cast_expression":
		return ctx.collectTypeNames(expr.ChildByFieldName("type"), uses)
	case "parenthesized_expression":
		inner := expr.ChildByFieldName("expression")
		if inner.IsNull() && expr.NamedChildCount() > 0 {
			inner = expr.NamedChild(0)
		}
		return ctx.inferExpressionTypeNames(inner, uses, current, properties, line)
	}
	return nil
}
