package php

import (
	"fmt"
	"sync"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
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

// ClassInfo describes a class declaration discovered in the file.
type ClassInfo struct {
	Name      string
	Extends   []string
	StartLine int
	EndLine   int
	StartByte uint32
}

// IndexedTree contains lightweight static analysis metadata for a PHP source file.
// It tracks properties, the types discovered for them, and variables scoped to
// functions or methods. A flattened type index is also provided for quick lookups.
type IndexedTree struct {
	Properties map[string][]TypeOccurrence
	Variables  map[string]FunctionScope
	Types      map[string][]TypeReference
	Classes    map[uint32]ClassInfo
}

// ByteRange represents a range of bytes in the source content.
type ByteRange struct {
	Start uint32
	End   uint32
}

// AnalyzeStatic builds an IndexedTree for the provided PHP source content and syntax tree.
// Both arguments are optional; a nil pointer yields an empty index.
func AnalyzeStatic(content *[]byte, tree *sitter.Tree) IndexedTree {
	analyzer := NewStaticAnalyzer()
	return analyzer.Update(content, tree, nil)
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
	content *[]byte
	tree    *sitter.Tree
	uses    map[string]string
}

func newAnalysisContext(content *[]byte, tree *sitter.Tree) *analysisContext {
	if content == nil || tree == nil {
		return nil
	}
	root := tree.RootNode()
	if root.IsNull() {
		return nil
	}
	ctx := &analysisContext{
		content: content,
		tree:    tree,
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
	mu    sync.Mutex
	index IndexedTree
	built bool
}

// NewStaticAnalyzer constructs an analyzer with an empty index.
func NewStaticAnalyzer() *StaticAnalyzer {
	return &StaticAnalyzer{
		index: IndexedTree{
			Properties: make(map[string][]TypeOccurrence),
			Variables:  make(map[string]FunctionScope),
			Types:      make(map[string][]TypeReference),
			Classes:    make(map[uint32]ClassInfo),
		},
	}
}

// Update recomputes the index, optionally reusing previous state for dirty ranges.
// When dirty is nil, the index is rebuilt from scratch.
func (a *StaticAnalyzer) Update(content *[]byte, tree *sitter.Tree, dirty []ByteRange) IndexedTree {
	a.mu.Lock()
	defer a.mu.Unlock()

	ctx := newAnalysisContext(content, tree)
	if ctx == nil {
		a.index = IndexedTree{
			Properties: make(map[string][]TypeOccurrence),
			Variables:  make(map[string]FunctionScope),
			Types:      make(map[string][]TypeReference),
			Classes:    make(map[uint32]ClassInfo),
		}
		a.built = false
		return a.index
	}

	if !a.built || len(dirty) == 0 {
		props := ctx.collectPropertyTypes()
		vars := ctx.collectFunctionVariableTypes(props)
		classes := ctx.collectClassInfo()
		a.index = IndexedTree{
			Properties: props,
			Variables:  vars,
			Types:      computeTypeReferences(props, vars),
			Classes:    classes,
		}
		a.built = true
		return a.index
	}

	props := clonePropertyIndex(a.index.Properties)
	vars := cloneFunctionIndex(a.index.Variables)
	classes := cloneClassIndex(a.index.Classes)

	index := ctx.updateIndex(props, vars, classes, dirty)
	a.index = index
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
