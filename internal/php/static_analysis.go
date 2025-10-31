package php

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	phpforest "github.com/alexaandru/go-sitter-forest/php"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/utils"
)

// SymbolKind indicates what kind of PHP symbol is associated with a type reference.
type SymbolKind string

const (
	// SymbolKindProperty marks references that originate from class properties.
	SymbolKindProperty SymbolKind = "property"
	// SymbolKindVariable marks references that originate from function-scoped variables.
	SymbolKindVariable SymbolKind = "variable"
)

// TypeOccurrence captures a single type assignment together with the line where it appears.
type TypeOccurrence struct {
	Type string
	Line int
}

// TypeReference ties a type name to the symbol (property or variable) where it was observed.
type TypeReference struct {
	Symbol string
	Kind   SymbolKind
	Line   int
}

// FunctionScope stores all variables indexed for a single function or method.
type FunctionScope struct {
	Variables map[string][]TypeOccurrence
	StartLine int
	EndLine   int
}

// LineColumnRange captures a range using 1-based lines and 0-based columns.
type LineColumnRange struct {
	StartLine   int
	StartColumn int
	EndLine     int
	EndColumn   int
}

// FunctionInfo captures metadata about a function or method declaration.
type FunctionInfo struct {
	URI        string
	Name       string
	Range      LineColumnRange
	Parameters LineColumnRange
	Body       LineColumnRange
}

type methodSet struct {
	private   []FunctionInfo
	protected []FunctionInfo
	public    []FunctionInfo
}

type externalClassData struct {
	methods *methodSet
	extends []string
}

// ClassInfo describes a class declaration discovered in the file.
type ClassInfo struct {
	Name      string
	Namespace string
	FQN       string
	Extends   []string
	StartLine int
	EndLine   int
	StartByte uint32
}

// IndexedTree contains lightweight static analysis metadata for a PHP source file.
// It tracks properties, the types discovered for them, and variables scoped to
// functions or methods. A flattened type index is also provided for quick lookups.
type IndexedTree struct {
	Properties         map[string][]TypeOccurrence
	Variables          map[string]FunctionScope
	Types              map[string][]TypeReference
	Classes            map[uint32]ClassInfo
	PrivateFunctions   []FunctionInfo
	ProtectedFunctions []FunctionInfo
	PublicFunctions    []FunctionInfo
}

// ByteRange represents a range of bytes in the source content.
type ByteRange struct {
	Start uint32
	End   uint32
}

func computeTypeReferences(properties map[string][]TypeOccurrence, functions map[string]FunctionScope) map[string][]TypeReference {
	result := make(map[string][]TypeReference)

	add := func(typeName, symbol string, kind SymbolKind, line int) {
		if typeName == "" {
			return
		}
		ref := TypeReference{
			Symbol: symbol,
			Kind:   kind,
			Line:   line,
		}
		result[typeName] = append(result[typeName], ref)
	}

	for property, occurrences := range properties {
		for _, occ := range occurrences {
			add(occ.Type, property, SymbolKindProperty, occ.Line)
		}
	}

	for functionName, scope := range functions {
		for variable, occurrences := range scope.Variables {
			for _, occ := range occurrences {
				symbol := functionName + "::" + variable
				add(occ.Type, symbol, SymbolKindVariable, occ.Line)
			}
		}
	}

	return result
}

type analysisContext struct {
	content  *[]byte
	tree     *sitter.Tree
	uses     map[string]string
	uri      string
	autoload config.AutoloadMap
	root     string
	loaded   map[string]externalClassData
}

func newAnalysisContext(content *[]byte, tree *sitter.Tree, uri string, autoload config.AutoloadMap, workspaceRoot string) *analysisContext {
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

// StaticAnalyzer incrementally maintains an IndexedTree for a PHP source file.
type StaticAnalyzer struct {
	mu       sync.Mutex
	index    IndexedTree
	autoload config.AutoloadMap
	uri      string
	root     string
	built    bool
}

// NewStaticAnalyzer constructs an analyzer with an empty index.
func NewStaticAnalyzer() *StaticAnalyzer {
	return &StaticAnalyzer{
		index: IndexedTree{
			Properties:         make(map[string][]TypeOccurrence),
			Variables:          make(map[string]FunctionScope),
			Types:              make(map[string][]TypeReference),
			Classes:            make(map[uint32]ClassInfo),
			PrivateFunctions:   nil,
			ProtectedFunctions: nil,
			PublicFunctions:    nil,
		},
	}
}

// Configure sets metadata consumed by the analyzer when producing function information.
func (a *StaticAnalyzer) Configure(uri string, autoload config.AutoloadMap, workspaceRoot string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.uri = uri
	a.autoload = autoload
	a.root = workspaceRoot
	a.applyURIToFunctionsLocked()
}

// Update recomputes the index, optionally reusing previous state for dirty ranges.
// When dirty is nil, the index is rebuilt from scratch.
func (a *StaticAnalyzer) Update(content *[]byte, tree *sitter.Tree, dirty []ByteRange) IndexedTree {
	a.mu.Lock()
	defer a.mu.Unlock()

	ctx := newAnalysisContext(content, tree, a.uri, a.autoload, a.root)
	if ctx == nil {
		a.index = IndexedTree{
			Properties:         make(map[string][]TypeOccurrence),
			Variables:          make(map[string]FunctionScope),
			Types:              make(map[string][]TypeReference),
			Classes:            make(map[uint32]ClassInfo),
			PrivateFunctions:   nil,
			ProtectedFunctions: nil,
			PublicFunctions:    nil,
		}
		a.applyURIToFunctionsLocked()
		a.built = false
		return a.index
	}

	if !a.built || len(dirty) == 0 {
		props := ctx.collectPropertyTypes()
		vars := ctx.collectFunctionVariableTypes(props)
		classes := ctx.collectClassInfo()
		priv, prot, pub := ctx.collectFunctionInfos(classes)
		a.index = IndexedTree{
			Properties:         props,
			Variables:          vars,
			Types:              computeTypeReferences(props, vars),
			Classes:            classes,
			PrivateFunctions:   priv,
			ProtectedFunctions: prot,
			PublicFunctions:    pub,
		}
		a.applyURIToFunctionsLocked()
		a.built = true
		return a.index
	}

	props := clonePropertyIndex(a.index.Properties)
	vars := cloneFunctionIndex(a.index.Variables)
	classes := cloneClassIndex(a.index.Classes)

	index := ctx.updateIndex(props, vars, classes, dirty)
	priv, prot, pub := ctx.collectFunctionInfos(index.Classes)
	index.PrivateFunctions = priv
	index.ProtectedFunctions = prot
	index.PublicFunctions = pub
	a.index = index
	a.applyURIToFunctionsLocked()
	return a.index
}

func clonePropertyIndex(in map[string][]TypeOccurrence) map[string][]TypeOccurrence {
	out := make(map[string][]TypeOccurrence, len(in))
	for k, v := range in {
		copied := make([]TypeOccurrence, len(v))
		copy(copied, v)
		out[k] = copied
	}
	return out
}

func cloneFunctionIndex(in map[string]FunctionScope) map[string]FunctionScope {
	out := make(map[string]FunctionScope, len(in))
	for k, v := range in {
		copied := make(map[string][]TypeOccurrence, len(v.Variables))
		for name, occs := range v.Variables {
			ref := make([]TypeOccurrence, len(occs))
			copy(ref, occs)
			copied[name] = ref
		}
		out[k] = FunctionScope{
			Variables: copied,
			StartLine: v.StartLine,
			EndLine:   v.EndLine,
		}
	}
	return out
}

func cloneClassIndex(in map[uint32]ClassInfo) map[uint32]ClassInfo {
	out := make(map[uint32]ClassInfo, len(in))
	for k, v := range in {
		extends := make([]string, len(v.Extends))
		copy(extends, v.Extends)
		out[k] = ClassInfo{
			Name:      v.Name,
			Namespace: v.Namespace,
			FQN:       v.FQN,
			Extends:   extends,
			StartLine: v.StartLine,
			EndLine:   v.EndLine,
			StartByte: v.StartByte,
		}
	}
	return out
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
			ctx.refreshPropertyDeclaration(cur, props)
		case "property_promotion_parameter":
			ctx.refreshPropertyPromotion(cur, props)
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

func rangeFromNode(node sitter.Node) LineColumnRange {
	if node.IsNull() {
		return LineColumnRange{}
	}
	start := node.StartPoint()
	end := node.EndPoint()
	return LineColumnRange{
		StartLine:   int(start.Row) + 1,
		StartColumn: int(start.Column),
		EndLine:     int(end.Row) + 1,
		EndColumn:   int(end.Column),
	}
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

func (ctx *analysisContext) loadExternalClass(simple, fqcn string, classMethods map[string]*methodSet, extendsMap map[string][]string, fullNames map[string]string) {
	data := ctx.ensureExternalClassLoaded(fqcn)
	if data.methods != nil {
		classMethods[simple] = data.methods
	}
	if len(data.extends) > 0 {
		extendsMap[simple] = cloneStrings(data.extends)
	}
	if fullNames != nil {
		fullNames[simple] = normalizeFQN(fqcn)
	}
}

func (ctx *analysisContext) externalExtendsFor(fqcn string) []string {
	data := ctx.ensureExternalClassLoaded(fqcn)
	return cloneStrings(data.extends)
}

func (ctx *analysisContext) ensureExternalClassLoaded(fqcn string) externalClassData {
	fqcn = normalizeFQN(fqcn)
	if fqcn == "" || ctx.autoload.IsEmpty() {
		return externalClassData{}
	}
	if ctx.loaded == nil {
		ctx.loaded = make(map[string]externalClassData)
	}
	if data, ok := ctx.loaded[fqcn]; ok {
		return data
	}
	path, ok := config.AutoloadResolve(fqcn, ctx.autoload, ctx.root)
	if !ok {
		ctx.loaded[fqcn] = externalClassData{}
		return ctx.loaded[fqcn]
	}
	dataBytes, err := os.ReadFile(path)
	if err != nil {
		ctx.loaded[fqcn] = externalClassData{}
		return ctx.loaded[fqcn]
	}
	parser := sitter.NewParser()
	lang := sitter.NewLanguage(phpforest.GetLanguage())
	_ = parser.SetLanguage(lang)
	tree, err := parser.ParseString(context.Background(), nil, dataBytes)
	if err != nil {
		ctx.loaded[fqcn] = externalClassData{}
		return ctx.loaded[fqcn]
	}
	defer tree.Close()

	copyBytes := append([]byte(nil), dataBytes...)
	uri := utils.PathToURI(path)
	extCtx := newAnalysisContext(&copyBytes, tree, uri, ctx.autoload, ctx.root)
	if extCtx == nil {
		ctx.loaded[fqcn] = externalClassData{}
		return ctx.loaded[fqcn]
	}

	extClasses := extCtx.collectClassInfo()
	extMethods, _, _ := extCtx.buildMethodMetadata(extClasses)
	for _, info := range extClasses {
		full := normalizeFQN(info.FQN)
		if full == "" {
			continue
		}
		entry := externalClassData{
			methods: extMethods[info.Name],
			extends: cloneStrings(info.Extends),
		}
		ctx.loaded[full] = entry
	}
	if data, ok := ctx.loaded[fqcn]; ok {
		return data
	}
	ctx.loaded[fqcn] = externalClassData{}
	return ctx.loaded[fqcn]
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func (a *StaticAnalyzer) applyURIToFunctionsLocked() {
	if a.uri == "" {
		return
	}

	setURI := func(fns []FunctionInfo) []FunctionInfo {
		for i := range fns {
			if fns[i].URI == "" {
				fns[i].URI = a.uri
			}
		}
		return fns
	}

	a.index.PrivateFunctions = setURI(a.index.PrivateFunctions)
	a.index.ProtectedFunctions = setURI(a.index.ProtectedFunctions)
	a.index.PublicFunctions = setURI(a.index.PublicFunctions)
}

func collectAncestorClasses(className string, extendsMap map[string][]string, available map[string]*methodSet) map[string]struct{} {
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
			if _, ok := ancestors[simple]; ok {
				continue
			}
			if _, ok := available[simple]; !ok {
				continue
			}
			ancestors[simple] = struct{}{}
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
