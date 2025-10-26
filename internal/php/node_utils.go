package php

import (
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

// VariableNameFromNode extracts the PHP variable identifier from the provided node.
func VariableNameFromNode(node sitter.Node, content []byte) string {
	if node.IsNull() {
		return ""
	}

	switch node.Type() {
	case "variable_name":
		for i := uint32(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child.Type() == "name" {
				return child.Content(content)
			}
		}
		raw := node.Content(content)
		return strings.TrimPrefix(raw, "$")
	case "by_ref":
		for i := uint32(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child.Type() == "variable_name" {
				return VariableNameFromNode(child, content)
			}
		}
	case "name":
		return node.Content(content)
	}

	raw := strings.TrimSpace(node.Content(content))
	return strings.TrimPrefix(raw, "$")
}

func memberAccessPropertyName(node sitter.Node, content []byte) string {
	if node.IsNull() {
		return ""
	}

	switch node.Type() {
	case "member_access_expression", "nullsafe_member_access_expression":
	default:
		return ""
	}

	objectNode := node.ChildByFieldName("object")
	if objectNode.IsNull() {
		return ""
	}

	switch objectNode.Type() {
	case "variable_name":
		if strings.TrimSpace(objectNode.Content(content)) != "$this" {
			return ""
		}
	default:
		return ""
	}

	nameNode := node.ChildByFieldName("name")
	if nameNode.IsNull() {
		return ""
	}

	return strings.TrimSpace(nameNode.Content(content))
}
