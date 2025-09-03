package analyzer

import (
	"bytes"
	"context"
	"slices"
	"sync"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	protocol "github.com/tliron/glsp/protocol_3_16"
	tsxml "github.com/tree-sitter-grammars/tree-sitter-xml/bindings/go"
)

type XmlAnalyzer interface {
	IsInServiceIDAttribute(pos protocol.Position) (bool, string)
}

type xmlAnalyzer struct {
	parser  *sitter.Parser
	mu      sync.RWMutex
	tree    *sitter.Tree
	content []byte
}

func NewXMLAnalyzer() Analyzer {
	p := sitter.NewParser()
	p.SetLanguage(sitter.NewLanguage(tsxml.LanguageXML()))
	return &xmlAnalyzer{parser: p}
}

func (a *xmlAnalyzer) Changed(code []byte, change *sitter.EditInput) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.content = code
	if a.tree != nil && change != nil {
		a.tree.Edit(*change)
	}
	newTree, err := a.parser.ParseCtx(context.Background(), a.tree, code)
	if err != nil {
		return err
	}
	if a.tree != nil {
		a.tree.Close()
	}
	a.tree = newTree
	return nil
}

func (a *xmlAnalyzer) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tree != nil {
		a.tree.Close()
		a.tree = nil
	}
}

func (a *xmlAnalyzer) IsInServiceIDAttribute(pos protocol.Position) (bool, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.tree == nil {
		return false, ""
	}

	point, ok := lspPosToPoint(pos, a.content)
	if !ok {
		return false, ""
	}

	root := a.tree.RootNode()
	if root == nil {
		return false, ""
	}

	node := root.NamedDescendantForPointRange(point, point)
	if node == nil {
		return false, ""
	}

	attr := a.ascendToType(node, "Attribute")
	if attr == nil {
		return false, ""
	}

	// Attribute name must be "id"
	if a.attributeName(attr) != "id" {
		return false, ""
	}

	// Find the owning tag
	tag := a.ascendToAny(node, "STag", "EmptyElemTag")
	if tag == nil {
		return false, ""
	}
	// Tag name should be "argument"
	if a.tagNameFromTagNode(tag) != "argument" {
		return false, ""
	}

	// Enforce ancestor chain: argument > service > services > container
	argumentEl := a.ascendToType(tag, "element")
	if argumentEl == nil {
		return false, ""
	}
	serviceEl := a.nearestAncestorElement(argumentEl.Parent())
	if serviceEl == nil || a.elementName(serviceEl) != "service" {
		return false, ""
	}
	servicesEl := a.nearestAncestorElement(serviceEl.Parent())
	if servicesEl == nil || a.elementName(servicesEl) != "services" {
		return false, ""
	}
	containerEl := a.nearestAncestorElement(servicesEl.Parent())
	if containerEl == nil || a.elementName(containerEl) != "container" {
		return false, ""
	}

	// Compute typed prefix inside id="..."
	prefix, ok := a.attributeValuePrefixAtCaret(attr, pos)
	if !ok {
		return false, ""
	}
	return true, prefix
}

func (a *xmlAnalyzer) ascendToType(n *sitter.Node, typ string) *sitter.Node {
	for cur := n; cur != nil; cur = cur.Parent() {
		if cur.Type() == typ {
			return cur
		}
	}
	return nil
}

func (a *xmlAnalyzer) ascendToAny(n *sitter.Node, types ...string) *sitter.Node {
	for cur := n; cur != nil; cur = cur.Parent() {
		ct := cur.Type()
		if slices.Contains(types, ct) {
			return cur
		}
	}
	return nil
}

func (a *xmlAnalyzer) nearestAncestorElement(n *sitter.Node) *sitter.Node {
	for cur := n; cur != nil; cur = cur.Parent() {
		if cur.Type() == "element" {
			return cur
		}
	}
	return nil
}

func (a *xmlAnalyzer) elementName(el *sitter.Node) string {
	if el == nil {
		return ""
	}
	var tag *sitter.Node
	for i := uint32(0); i < el.NamedChildCount(); i++ {
		child := el.NamedChild(int(i))
		if child == nil {
			continue
		}
		switch child.Type() {
		case "STag", "EmptyElemTag":
			tag = child
		}
		if tag != nil {
			break
		}
	}
	if tag == nil {
		return ""
	}
	return a.tagNameFromTagNode(tag)
}

func (a *xmlAnalyzer) attributeName(attr *sitter.Node) string {
	for i := uint32(0); i < attr.NamedChildCount(); i++ {
		child := attr.NamedChild(int(i))
		if child != nil && child.Type() == "Name" {
			return child.Content(a.content)
		}
	}
	text := a.content[attr.StartByte():attr.EndByte()]
	text = bytes.TrimSpace(text)
	// Extract up to whitespace or '='
	i := 0
	for i < len(text) && !unicode.IsSpace(rune(text[i])) && text[i] != '=' {
		i++
	}
	return string(text[:i])
}

func (a *xmlAnalyzer) tagNameFromTagNode(tag *sitter.Node) string {
	for i := uint32(0); i < tag.NamedChildCount(); i++ {
		child := tag.NamedChild(int(i))
		if child != nil && child.Type() == "Name" {
			return child.Content(a.content)
		}
	}
	raw := []byte(tag.Content(a.content))
	j := 0
	for j < len(raw) && raw[j] != '<' {
		j++
	}
	for j < len(raw) && (raw[j] == '<' || raw[j] == '/') {
		j++
	}
	k := j
	for k < len(raw) && a.isNameChar(raw[k]) {
		k++
	}
	return string(raw[j:k])
}

func (a *xmlAnalyzer) isNameChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	}
	switch b {
	case '-', '_', '.', ':':
		return true
	default:
		return false
	}
}

func (a *xmlAnalyzer) attributeValuePrefixAtCaret(attr *sitter.Node, pos protocol.Position) (string, bool) {
	caret := lspPosToByteOffset(a.content, pos)
	if caret < 0 {
		return "", false
	}

	start := int(attr.StartByte())
	end := int(attr.EndByte())
	if start >= end || start >= len(a.content) {
		return "", false
	}
	if end > len(a.content) {
		end = len(a.content)
	}
	segment := a.content[start:end]

	// Finding '='
	eq := bytes.IndexByte(segment, '=')
	if eq == -1 {
		return "", false
	}

	// Only crazy people put whitespace after their =, but let's skip anyway
	i := eq + 1
	for i < len(segment) && (segment[i] == ' ' || segment[i] == '\t' || segment[i] == '\n' || segment[i] == '\r') {
		i++
	}
	if i >= len(segment) {
		return "", false
	}
	q := segment[i]
	if q != '"' && q != '\'' {
		// We need quotes
		return "", false
	}
	valStart := start + i + 1

	// Find closing quote
	jrel := bytes.IndexByte(segment[i+1:], q)
	valEnd := end
	if jrel != -1 {
		valEnd = start + i + 1 + jrel
	}

	// Need to be in range
	if caret < valStart || caret > valEnd {
		return "", false
	}

	return string(a.content[valStart:caret]), true
}
