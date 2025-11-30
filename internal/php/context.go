package php

import (
	"fmt"
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
)

type analysisContext struct {
	content  *[]byte
	tree     *sitter.Tree
	uses     map[string]string
	uri      string
	autoload config.AutoloadMap
	root     string
	loaded   map[string]externalClassData
	store    *DocumentStore
}

func newAnalysisContext(content *[]byte, tree *sitter.Tree, uri string, autoload config.AutoloadMap, workspaceRoot string, store *DocumentStore) *analysisContext {
	if content == nil || tree == nil {
		return nil
	}
	root := tree.RootNode()
	if root.IsNull() {
		return nil
	}
	ctx := &analysisContext{
		content:  content,
		tree:     tree,
		uri:      uri,
		autoload: autoload,
		root:     workspaceRoot,
		loaded:   make(map[string]externalClassData),
		store:    store,
	}
	ctx.uses = ctx.collectNamespaceUses(ctx.rootNode())
	return ctx
}

func (ctx *analysisContext) bytes() []byte {
	if ctx == nil || ctx.content == nil {
		return nil
	}
	return *ctx.content
}

func (ctx *analysisContext) rootNode() sitter.Node {
	if ctx == nil || ctx.tree == nil {
		return sitter.Node{}
	}
	return ctx.tree.RootNode()
}

func (ctx *analysisContext) updateIndex(props map[string][]TypeOccurrence, vars map[string]FunctionScope, classes map[uint32]ClassInfo, dirty []ByteRange) IndexedTree {
	root := ctx.rootNode()
	if root.IsNull() {
		return IndexedTree{
			Properties: props,
			Variables:  vars,
			Types:      computeTypeReferences(props, vars),
			Classes:    classes,
		}
	}

	visited := make(map[string]struct{})
	for _, r := range dirty {
		start := int(r.Start)
		end := int(r.End)
		if end < start {
			start, end = end, start
		}
		node := root.NamedDescendantForByteRange(uint32(start), uint32(end))
		if ctx.refreshForNode(node, visited, props, vars, classes) {
			// Fallback to full rebuild when incremental update is insufficient.
			freshProps := ctx.collectPropertyTypes()
			freshVars := ctx.collectFunctionVariableTypes(freshProps)
			freshClasses := ctx.collectClassInfo()
			return IndexedTree{
				Properties: freshProps,
				Variables:  freshVars,
				Types:      computeTypeReferences(freshProps, freshVars),
				Classes:    freshClasses,
			}
		}
	}

	index := IndexedTree{
		Properties: props,
		Variables:  vars,
		Types:      computeTypeReferences(props, vars),
		Classes:    classes,
	}
	return index
}

func (ctx *analysisContext) refreshForNode(node sitter.Node, visited map[string]struct{}, props map[string][]TypeOccurrence, vars map[string]FunctionScope, classes map[uint32]ClassInfo) bool {
	for cur := node; !cur.IsNull(); cur = cur.Parent() {
		typeName := cur.Type()
		switch typeName {
		case "program":
			return false
		case "namespace_use_declaration", "namespace_use_clause", "namespace_use_group":
			return true
		}

		key := fmt.Sprintf("%s#%d", typeName, cur.StartByte())
		if _, ok := visited[key]; ok {
			continue
		}
		visited[key] = struct{}{}

		switch typeName {
		case "property_declaration":
			return true
		case "property_promotion_parameter":
			return true
		case "method_declaration", "function_definition", "function_declaration":
			ctx.refreshFunctionScope(cur, props, vars)
		case "class_declaration":
			ctx.refreshClassDeclaration(cur, classes)
		}
	}
	return false
}

func (ctx *analysisContext) collectFunctionInfos(classes map[uint32]ClassInfo) ([]FunctionInfo, []FunctionInfo, []FunctionInfo) {
	if ctx == nil || len(classes) == 0 {
		return nil, nil, nil
	}

	classMethods, extendsMap, fullNames := ctx.buildMethodMetadata(classes)
	if len(classMethods) == 0 {
		return nil, nil, nil
	}

	privateFns := make([]FunctionInfo, 0)
	protectedFns := make([]FunctionInfo, 0)
	publicFns := make([]FunctionInfo, 0)

	for className, methods := range classMethods {
		if className == "" || methods == nil {
			continue
		}

		ownProtectedNames := make(map[string]struct{}, len(methods.protected))
		ownPublicNames := make(map[string]struct{}, len(methods.public))
		for _, fn := range methods.protected {
			ownProtectedNames[fn.Name] = struct{}{}
		}
		for _, fn := range methods.public {
			ownPublicNames[fn.Name] = struct{}{}
		}

		prefPrivate := prefixFunctionInfos(className, methods.private)
		privateFns = append(privateFns, prefPrivate...)

		prefProtected := prefixFunctionInfos(className, methods.protected)
		protectedFns = append(protectedFns, prefProtected...)

		prefPublic := prefixFunctionInfos(className, methods.public)
		publicFns = append(publicFns, prefPublic...)

		addedProtected := make(map[string]struct{}, len(prefProtected))
		for _, fn := range prefProtected {
			addedProtected[fn.Name] = struct{}{}
		}
		addedPublic := make(map[string]struct{}, len(prefPublic))
		for _, fn := range prefPublic {
			addedPublic[fn.Name] = struct{}{}
		}

		ancestors := ctx.collectAncestorClasses(className, extendsMap, classMethods, fullNames)
		for ancestor := range ancestors {
			ancestorMethods, ok := classMethods[ancestor]
			if !ok || ancestorMethods == nil {
				continue
			}
			for _, fn := range ancestorMethods.protected {
				if _, exists := ownProtectedNames[fn.Name]; exists {
					continue
				}
				pref := prefixFunctionInfo(className, fn)
				if _, exists := addedProtected[pref.Name]; exists {
					continue
				}
				protectedFns = append(protectedFns, pref)
				addedProtected[pref.Name] = struct{}{}
			}
			for _, fn := range ancestorMethods.public {
				if _, exists := ownPublicNames[fn.Name]; exists {
					continue
				}
				pref := prefixFunctionInfo(className, fn)
				if _, exists := addedPublic[pref.Name]; exists {
					continue
				}
				publicFns = append(publicFns, pref)
				addedPublic[pref.Name] = struct{}{}
			}
		}
	}

	return privateFns, protectedFns, publicFns
}

func (ctx *analysisContext) buildMethodMetadata(classes map[uint32]ClassInfo) (map[string]*methodSet, map[string][]string, map[string]string) {
	classMethods := make(map[string]*methodSet, len(classes))
	extendsMap := make(map[string][]string, len(classes))
	fullNames := make(map[string]string, len(classes))
	startToClass := make(map[uint32]ClassInfo, len(classes))

	for start, info := range classes {
		if info.Name == "" {
			continue
		}
		startToClass[start] = info
		extendsMap[info.Name] = info.Extends
		if info.FQN != "" {
			fullNames[info.Name] = info.FQN
		}
		if _, ok := classMethods[info.Name]; !ok {
			classMethods[info.Name] = &methodSet{}
		}
	}

	content := ctx.bytes()
	root := ctx.rootNode()
	if root.IsNull() {
		return classMethods, extendsMap, fullNames
	}

	stack := []sitter.Node{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if node.Type() == "class_declaration" {
			info, ok := startToClass[uint32(node.StartByte())]
			if ok && info.Name != "" {
				methods := classMethods[info.Name]
				if methods == nil {
					methods = &methodSet{}
					classMethods[info.Name] = methods
				}
				body := node.ChildByFieldName("body")
				if !body.IsNull() {
					for i := uint32(0); i < body.NamedChildCount(); i++ {
						child := body.NamedChild(i)
						if child.Type() != "method_declaration" {
							continue
						}
						fn, visibility, okMethod := functionInfoFromMethod(child, content, ctx.uri)
						if !okMethod {
							continue
						}
						switch visibility {
						case "private":
							methods.private = append(methods.private, fn)
						case "protected":
							methods.protected = append(methods.protected, fn)
						default:
							methods.public = append(methods.public, fn)
						}
					}
				}
			}
		}

		for i := uint32(0); i < node.NamedChildCount(); i++ {
			stack = append(stack, node.NamedChild(i))
		}
	}

	return classMethods, extendsMap, fullNames
}

func (ctx *analysisContext) collectAncestorClasses(className string, extendsMap map[string][]string, available map[string]*methodSet, fullNames map[string]string) map[string]struct{} {
	ancestors := make(map[string]struct{})
	visited := make(map[string]struct{})

	var visit func(string)
	visit = func(current string) {
		bases := extendsMap[current]
		for _, base := range bases {
			simple := simpleClassName(base)
			if simple == "" {
				continue
			}
			if _, ok := available[simple]; !ok {
				ctx.loadExternalClass(simple, base, available, extendsMap, fullNames)
			}
			if _, ok := available[simple]; !ok {
				continue
			}
			if _, ok := ancestors[simple]; !ok {
				ancestors[simple] = struct{}{}
			}
			if _, seen := visited[simple]; !seen {
				visited[simple] = struct{}{}
				visit(simple)
			}
		}
	}

	visit(className)
	return ancestors
}

func simpleClassName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.TrimPrefix(name, "\\")
	if idx := strings.LastIndex(name, "\\"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func prefixFunctionInfos(className string, functions []FunctionInfo) []FunctionInfo {
	if len(functions) == 0 {
		return nil
	}
	prefixed := make([]FunctionInfo, 0, len(functions))
	for _, fn := range functions {
		prefixed = append(prefixed, prefixFunctionInfo(className, fn))
	}
	return prefixed
}

func prefixFunctionInfo(className string, fn FunctionInfo) FunctionInfo {
	pref := fn
	pref.Name = fmt.Sprintf("%s::%s", className, fn.Name)
	return pref
}

func functionInfoFromMethod(node sitter.Node, content []byte, uri string) (FunctionInfo, string, bool) {
	if node.IsNull() || node.Type() != "method_declaration" {
		return FunctionInfo{}, "", false
	}

	nameNode := node.ChildByFieldName("name")
	if nameNode.IsNull() {
		return FunctionInfo{}, "", false
	}

	methodName := strings.TrimSpace(nameNode.Content(content))
	if methodName == "" {
		return FunctionInfo{}, "", false
	}

	info := FunctionInfo{
		URI:        uri,
		Name:       methodName,
		Range:      rangeFromNode(nameNode),
		Parameters: rangeFromNode(node.ChildByFieldName("parameters")),
		Body:       rangeFromNode(node.ChildByFieldName("body")),
	}

	visibility := "public"
	for i := uint32(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Type() == "visibility_modifier" {
			candidate := strings.ToLower(strings.TrimSpace(child.Content(content)))
			switch candidate {
			case "private", "protected", "public":
				visibility = candidate
			}
			break
		}
	}

	return info, visibility, true
}
