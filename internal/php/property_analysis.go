package php

import (
	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

func (ctx *analysisContext) collectPropertyTypes() map[string][]TypeOccurrence {
	types := make(map[string][]TypeOccurrence)
	root := ctx.rootNode()
	if root.IsNull() {
		return types
	}

	uses := ctx.collectNamespaceUses(root)
	stack := []sitter.Node{root}

	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		switch node.Type() {
		case "property_declaration":
			for name, collected := range ctx.propertyTypesFromDeclaration(node, uses) {
				if len(collected) == 0 {
					continue
				}
				types[name] = mergeTypeOccurrences(types[name], collected)
			}
		case "property_promotion_parameter":
			if name, collected, ok := ctx.propertyTypeFromPromotion(node, uses); ok && len(collected) > 0 {
				types[name] = mergeTypeOccurrences(types[name], collected)
			}
		}

		for i := node.NamedChildCount(); i > 0; i-- {
			stack = append(stack, node.NamedChild(i-1))
		}
	}

	return types
}

func (ctx *analysisContext) propertyTypesFromDeclaration(node sitter.Node, uses map[string]string) map[string][]TypeOccurrence {
	result := make(map[string][]TypeOccurrence)
	content := ctx.bytes()

	typeNode := node.ChildByFieldName("type")
	if typeNode.IsNull() {
		return result
	}

	typeNames := ctx.collectTypeNames(typeNode, uses)
	if len(typeNames) == 0 {
		return result
	}

	for i := uint32(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Type() != "property_element" {
			continue
		}
		line := int(child.StartPoint().Row) + 1
		nameNode := child.ChildByFieldName("name")
		name := VariableNameFromNode(nameNode, content)
		if name == "" {
			continue
		}
		occ := make([]TypeOccurrence, 0, len(typeNames))
		for _, typ := range typeNames {
			occ = append(occ, TypeOccurrence{Type: typ, Line: line})
		}
		result[name] = append(result[name], occ...)
	}

	return result
}

func (ctx *analysisContext) propertyTypeFromPromotion(node sitter.Node, uses map[string]string) (string, []TypeOccurrence, bool) {
	content := ctx.bytes()

	typeNode := node.ChildByFieldName("type")
	if typeNode.IsNull() {
		return "", nil, false
	}

	typeNames := ctx.collectTypeNames(typeNode, uses)
	if len(typeNames) == 0 {
		return "", nil, false
	}

	nameNode := node.ChildByFieldName("name")
	name := VariableNameFromNode(nameNode, content)
	if name == "" {
		return "", nil, false
	}

	line := int(node.StartPoint().Row) + 1
	occ := make([]TypeOccurrence, 0, len(typeNames))
	for _, typ := range typeNames {
		occ = append(occ, TypeOccurrence{Type: typ, Line: line})
	}

	return name, occ, true
}
