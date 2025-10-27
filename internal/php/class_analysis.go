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

	ctx.expandClassExtends(result)
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
	namespace := ctx.namespaceForNode(node)
	fqn := name
	if name != "" && namespace != "" {
		fqn = namespace + "\\" + strings.TrimLeft(name, "\\")
	}
	fqn = normalizeFQN(fqn)

	startLine := int(node.StartPoint().Row) + 1
	endLine := int(node.EndPoint().Row) + 1
	startByte := uint32(node.StartByte())
	extends := ctx.classExtendsFromNode(node, namespace)

	return ClassInfo{
		Name:      name,
		Namespace: namespace,
		FQN:       fqn,
		Extends:   extends,
		StartLine: startLine,
		EndLine:   endLine,
		StartByte: startByte,
	}, true
}

func (ctx *analysisContext) classExtendsFromNode(node sitter.Node, namespace string) []string {
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
			resolved := ctx.qualifyClassName(candidate, namespace, uses)
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

func (ctx *analysisContext) namespaceForNode(node sitter.Node) string {
	for cur := node; !cur.IsNull(); cur = cur.Parent() {
		switch cur.Type() {
		case "namespace_definition", "namespace_declaration":
			if nameNode := cur.ChildByFieldName("name"); !nameNode.IsNull() {
				ns := strings.TrimSpace(nameNode.Content(ctx.bytes()))
				return normalizeFQN(ns)
			}
		}
	}
	return ctx.namespaceBefore(uint32(node.StartByte()))
}

func (ctx *analysisContext) qualifyClassName(name, namespace string, uses map[string]string) string {
	resolved := ctx.resolveRawTypeName(name, uses)
	if resolved == "" {
		resolved = name
	}
	resolved = normalizeFQN(resolved)
	if resolved == "" {
		return ""
	}
	if strings.Contains(resolved, "\\") {
		return resolved
	}
	if namespace != "" {
		return normalizeFQN(namespace + "\\" + resolved)
	}
	return resolved
}

func (ctx *analysisContext) namespaceBefore(bytePos uint32) string {
	root := ctx.rootNode()
	if root.IsNull() {
		return ""
	}
	current := ""
	for i := uint32(0); i < root.NamedChildCount(); i++ {
		child := root.NamedChild(i)
		if uint32(child.StartByte()) >= bytePos {
			break
		}
		if child.Type() == "namespace_definition" || child.Type() == "namespace_declaration" {
			if nameNode := child.ChildByFieldName("name"); !nameNode.IsNull() {
				ns := strings.TrimSpace(nameNode.Content(ctx.bytes()))
				current = normalizeFQN(ns)
			}
		}
	}
	return current
}

func (ctx *analysisContext) expandClassExtends(classes map[uint32]ClassInfo) {
	if len(classes) == 0 {
		return
	}
	direct := make(map[string][]string, len(classes))
	for _, info := range classes {
		if info.FQN == "" {
			continue
		}
		direct[strings.ToLower(info.FQN)] = cloneStrings(info.Extends)
	}
	for key, info := range classes {
		info.Extends = ctx.collectAllAncestors(info.Extends, direct)
		if info.FQN != "" {
			direct[strings.ToLower(info.FQN)] = cloneStrings(info.Extends)
		}
		classes[key] = info
	}
}

func (ctx *analysisContext) collectAllAncestors(initial []string, direct map[string][]string) []string {
	if len(initial) == 0 {
		return nil
	}
	queue := append([]string(nil), initial...)
	seen := make(map[string]struct{}, len(initial))
	var result []string

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		cur = normalizeFQN(cur)
		if cur == "" {
			continue
		}
		lower := strings.ToLower(cur)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		result = append(result, cur)

		if next, ok := direct[lower]; ok {
			queue = append(queue, next...)
			continue
		}
		if next := ctx.externalExtendsFor(cur); len(next) > 0 {
			queue = append(queue, next...)
		}
	}

	return result
}
