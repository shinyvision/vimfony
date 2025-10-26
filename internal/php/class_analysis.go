package php

import (
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

func (ctx *analysisContext) collectClassInfo() map[uint32]ClassInfo {
	result := make(map[uint32]ClassInfo)
	root := ctx.rootNode()
	if root.IsNull() {
		return result
	}

	stack := []sitter.Node{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if node.Type() == "class_declaration" {
			if info, ok := ctx.classInfoFromNode(node); ok {
				result[info.StartByte] = info
			}
		}

		for i := uint32(0); i < node.NamedChildCount(); i++ {
			stack = append(stack, node.NamedChild(i))
		}
	}

	return result
}

func (ctx *analysisContext) classInfoFromNode(node sitter.Node) (ClassInfo, bool) {
	if node.IsNull() || node.Type() != "class_declaration" {
		return ClassInfo{}, false
	}

	content := ctx.bytes()
	name := ""
	if nameNode := node.ChildByFieldName("name"); !nameNode.IsNull() {
		name = strings.TrimSpace(nameNode.Content(content))
	}

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1
	startByte := uint32(node.StartByte())
	extends := ctx.classExtendsFromNode(node)

	return ClassInfo{
		Name:      name,
		Extends:   extends,
		StartLine: startLine,
		EndLine:   endLine,
		StartByte: startByte,
	}, true
}

func (ctx *analysisContext) classExtendsFromNode(node sitter.Node) []string {
	content := ctx.bytes()
	uses := ctx.uses
	seen := make(map[string]struct{})
	var result []string

	for i := uint32(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Type() != "base_clause" {
			continue
		}
		for j := uint32(0); j < child.NamedChildCount(); j++ {
			base := child.NamedChild(j)
			candidate := strings.TrimSpace(base.Content(content))
			if candidate == "" {
				continue
			}
			resolved := ctx.resolveRawTypeName(candidate, uses)
			if resolved == "" {
				resolved = candidate
			}
			resolved = normalizeFQN(resolved)
			if resolved == "" {
				continue
			}
			key := strings.ToLower(resolved)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, resolved)
		}
	}

	return result
}

func (ctx *analysisContext) refreshClassDeclaration(node sitter.Node, classes map[uint32]ClassInfo) {
	if info, ok := ctx.classInfoFromNode(node); ok {
		classes[info.StartByte] = info
	} else {
		delete(classes, uint32(node.StartByte()))
	}
}
