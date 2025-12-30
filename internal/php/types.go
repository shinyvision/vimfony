package php

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
	Uses               map[string]string
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
