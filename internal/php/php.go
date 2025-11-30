package php

import (
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// PathAt returns the PHP class name or fully qualified name at the given position.
func PathAt(store *DocumentStore, path string, pos protocol.Position) (string, bool) {
	if store == nil {
		return "", false
	}
	doc, err := store.Get(path)
	if err != nil {
		return "", false
	}

	var result string
	var found bool

	doc.Read(func(tree *sitter.Tree, content []byte, _ IndexedTree) {
		root := tree.RootNode()
		if root.IsNull() {
			return
		}
		point := sitter.Point{Row: uint(pos.Line), Column: uint(pos.Character)}
		node := root.NamedDescendantForPointRange(point, point)

		var candidate sitter.Node
		for cur := node; !cur.IsNull(); cur = cur.Parent() {
			if cur.Type() == "qualified_name" {
				result = cur.Content(content)
				found = true
				return
			}
			if cur.Type() == "name" && candidate.IsNull() {
				candidate = cur
			}
		}

		if !candidate.IsNull() {
			result = candidate.Content(content)
			found = true
		}
	})

	return result, found
}

// Resolve locates the file defining the given class and returns its path and the range of the class definition.
func Resolve(store *DocumentStore, className string) (string, protocol.Range, bool) {
	if store == nil {
		return "", protocol.Range{}, false
	}
	autoloadMap, workspaceRoot := store.Config()
	path, ok := config.AutoloadResolve(className, autoloadMap, workspaceRoot)
	if !ok {
		return "", protocol.Range{}, false
	}

	doc, err := store.Get(path)
	if err != nil {
		return path, protocol.Range{}, true // Found file but failed to parse/load
	}

	var rng protocol.Range
	var found bool

	doc.Read(func(tree *sitter.Tree, content []byte, _ IndexedTree) {
		root := tree.RootNode()
		targetName := simpleClassName(className)
		var foundNode sitter.Node

		var findClass func(n sitter.Node)
		findClass = func(n sitter.Node) {
			if !foundNode.IsNull() {
				return
			}
			t := n.Type()
			if t == "class_declaration" || t == "interface_declaration" || t == "trait_declaration" {
				nameNode := n.ChildByFieldName("name")
				if !nameNode.IsNull() && nameNode.Content(content) == targetName {
					foundNode = n
					return
				}
			}
			for i := uint32(0); i < n.NamedChildCount(); i++ {
				findClass(n.NamedChild(i))
			}
		}
		findClass(root)

		if !foundNode.IsNull() {
			nameNode := foundNode.ChildByFieldName("name")
			if !nameNode.IsNull() {
				r := rangeFromNode(nameNode)
				rng = protocol.Range{
					Start: protocol.Position{Line: uint32(r.StartLine - 1), Character: uint32(r.StartColumn)},
					End:   protocol.Position{Line: uint32(r.EndLine - 1), Character: uint32(r.EndColumn)},
				}
				found = true
			}
		}
	})

	return path, rng, found
}

// FindMethodRange locates the definition of a method within a file.
func FindMethodRange(store *DocumentStore, path, methodName string) (protocol.Range, bool) {
	if store == nil {
		return protocol.Range{}, false
	}
	doc, err := store.Get(path)
	if err != nil {
		return protocol.Range{}, false
	}

	var rng protocol.Range
	var found bool

	doc.Read(func(tree *sitter.Tree, content []byte, _ IndexedTree) {
		root := tree.RootNode()
		var foundNode sitter.Node

		var findMethod func(n sitter.Node)
		findMethod = func(n sitter.Node) {
			if !foundNode.IsNull() {
				return
			}
			if n.Type() == "method_declaration" {
				nameNode := n.ChildByFieldName("name")
				if !nameNode.IsNull() && strings.EqualFold(nameNode.Content(content), methodName) {
					foundNode = nameNode
					return
				}
			}
			for i := uint32(0); i < n.NamedChildCount(); i++ {
				findMethod(n.NamedChild(i))
			}
		}
		findMethod(root)

		if !foundNode.IsNull() {
			r := rangeFromNode(foundNode)
			rng = protocol.Range{
				Start: protocol.Position{Line: uint32(r.StartLine - 1), Character: uint32(r.StartColumn)},
				End:   protocol.Position{Line: uint32(r.EndLine - 1), Character: uint32(r.EndColumn)},
			}
			found = true
		}
	})

	return rng, found
}
