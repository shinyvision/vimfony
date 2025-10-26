package php

import (
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
}

// IndexedTree contains lightweight static analysis metadata for a PHP source file.
// It tracks properties, the types discovered for them, and variables scoped to
// functions or methods. A flattened type index is also provided for quick lookups.
type IndexedTree struct {
	Properties map[string][]TypeOccurrence
	Variables  map[string]FunctionScope
	Types      map[string][]TypeReference
}

// AnalyzeStatic builds an IndexedTree for the provided PHP source content and syntax tree.
// Both arguments are optional; a nil pointer yields an empty index.
func AnalyzeStatic(content *[]byte, tree *sitter.Tree) IndexedTree {
	index := IndexedTree{
		Properties: make(map[string][]TypeOccurrence),
		Variables:  make(map[string]FunctionScope),
		Types:      make(map[string][]TypeReference),
	}

	ctx := newAnalysisContext(content, tree)
	if ctx == nil {
		return index
	}

	properties := ctx.collectPropertyTypes()
	variables := ctx.collectFunctionVariableTypes(properties)

	index.Properties = properties
	index.Variables = variables
	index.Types = computeTypeReferences(index.Properties, index.Variables)

	return index
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
}

func newAnalysisContext(content *[]byte, tree *sitter.Tree) *analysisContext {
	if content == nil || tree == nil {
		return nil
	}
	root := tree.RootNode()
	if root.IsNull() {
		return nil
	}
	return &analysisContext{
		content: content,
		tree:    tree,
	}
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
