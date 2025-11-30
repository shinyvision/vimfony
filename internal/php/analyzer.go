package php

import (
	"sync"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
)

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
func (a *StaticAnalyzer) Update(content *[]byte, tree *sitter.Tree, dirty []ByteRange, store *DocumentStore) IndexedTree {
	a.mu.Lock()
	defer a.mu.Unlock()

	ctx := newAnalysisContext(content, tree, a.uri, a.autoload, a.root, store)
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
