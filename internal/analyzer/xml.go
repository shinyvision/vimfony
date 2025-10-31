package analyzer

import (
	"bytes"
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"

	tsxml "github.com/alexaandru/go-sitter-forest/xml"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/twig"
	"github.com/shinyvision/vimfony/internal/utils"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type xmlAnalyzer struct {
	parser    *sitter.Parser
	mu        sync.RWMutex
	tree      *sitter.Tree
	content   []byte
	container *config.ContainerConfig
	autoload  config.AutoloadMap
}

func NewXMLAnalyzer() Analyzer {
	p := sitter.NewParser()
	_ = p.SetLanguage(sitter.NewLanguage(tsxml.GetLanguage()))
	return &xmlAnalyzer{parser: p}
}

func (a *xmlAnalyzer) Changed(code []byte, change *sitter.InputEdit) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.content = code
	if a.tree != nil && change != nil {
		a.tree.Edit(*change)
	}
	newTree, err := a.parser.ParseString(context.Background(), a.tree, code)
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

func (a *xmlAnalyzer) isInServiceIDAttribute(pos protocol.Position) (bool, string) {
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
	if root.IsNull() {
		return false, ""
	}

	node := root.NamedDescendantForPointRange(point, point)
	if node.IsNull() {
		return false, ""
	}

	attr := a.ascendToType(node, "Attribute")
	if attr.IsNull() {
		return false, ""
	}

	if a.attributeName(attr) != "id" {
		return false, ""
	}

	tag := a.ascendToAny(node, "STag", "EmptyElemTag")
	if tag.IsNull() {
		return false, ""
	}
	if a.tagNameFromTagNode(tag) != "argument" {
		return false, ""
	}

	argumentEl := a.ascendToType(tag, "element")
	if argumentEl.IsNull() {
		return false, ""
	}
	serviceEl := a.nearestAncestorElement(argumentEl.Parent())
	if serviceEl.IsNull() || a.elementName(serviceEl) != "service" {
		return false, ""
	}
	servicesEl := a.nearestAncestorElement(serviceEl.Parent())
	if servicesEl.IsNull() || a.elementName(servicesEl) != "services" {
		return false, ""
	}
	containerEl := a.nearestAncestorElement(servicesEl.Parent())
	if containerEl.IsNull() || a.elementName(containerEl) != "container" {
		return false, ""
	}

	prefix, ok := a.attributeValuePrefixAtCaret(attr, pos)
	if !ok {
		return false, ""
	}
	return true, prefix
}

func (a *xmlAnalyzer) ascendToType(n sitter.Node, typ string) sitter.Node {
	for cur := n; !cur.IsNull(); cur = cur.Parent() {
		if cur.Type() == typ {
			return cur
		}
	}
	return sitter.Node{}
}

func (a *xmlAnalyzer) ascendToAny(n sitter.Node, types ...string) sitter.Node {
	for cur := n; !cur.IsNull(); cur = cur.Parent() {
		if slices.Contains(types, cur.Type()) {
			return cur
		}
	}
	return sitter.Node{}
}

func (a *xmlAnalyzer) nearestAncestorElement(n sitter.Node) sitter.Node {
	for cur := n; !cur.IsNull(); cur = cur.Parent() {
		if cur.Type() == "element" {
			return cur
		}
	}
	return sitter.Node{}
}

func (a *xmlAnalyzer) elementName(el sitter.Node) string {
	if el.IsNull() {
		return ""
	}
	var tag sitter.Node
	for i := uint32(0); i < el.NamedChildCount(); i++ {
		child := el.NamedChild(i)
		if !child.IsNull() {
			switch child.Type() {
			case "STag", "EmptyElemTag":
				tag = child
			}
		}
		if !tag.IsNull() {
			break
		}
	}
	if tag.IsNull() {
		return ""
	}
	return a.tagNameFromTagNode(tag)
}

func (a *xmlAnalyzer) attributeName(attr sitter.Node) string {
	for i := uint32(0); i < attr.NamedChildCount(); i++ {
		child := attr.NamedChild(i)
		if !child.IsNull() && child.Type() == "Name" {
			return child.Content(a.content)
		}
	}
	text := a.content[attr.StartByte():attr.EndByte()]
	text = bytes.TrimSpace(text)
	i := 0
	for i < len(text) && !unicode.IsSpace(rune(text[i])) && text[i] != '=' {
		i++
	}
	return string(text[:i])
}

func (a *xmlAnalyzer) tagNameFromTagNode(tag sitter.Node) string {
	for i := uint32(0); i < tag.NamedChildCount(); i++ {
		child := tag.NamedChild(i)
		if !child.IsNull() && child.Type() == "Name" {
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

func (a *xmlAnalyzer) attributeValuePrefixAtCaret(attr sitter.Node, pos protocol.Position) (string, bool) {
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
	eq := bytes.IndexByte(segment, '=')
	if eq == -1 {
		return "", false
	}
	i := eq + 1
	for i < len(segment) && (segment[i] == ' ' || segment[i] == '\t' || segment[i] == '\n' || segment[i] == '\r') {
		i++
	}
	if i >= len(segment) {
		return "", false
	}
	q := segment[i]
	if q != '"' && q != '\'' {
		return "", false
	}
	valStart := start + i + 1
	jrel := bytes.IndexByte(segment[i+1:], q)
	valEnd := end
	if jrel != -1 {
		valEnd = start + i + 1 + jrel
	}
	if caret < valStart || caret > valEnd {
		return "", false
	}
	return string(a.content[valStart:caret]), true
}

func (a *xmlAnalyzer) SetContainerConfig(container *config.ContainerConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.container = container
}

func (a *xmlAnalyzer) SetAutoloadMap(autoload *config.AutoloadMap) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if autoload == nil {
		a.autoload = config.AutoloadMap{}
		return
	}
	a.autoload = *autoload
}

func (a *xmlAnalyzer) OnCompletion(pos protocol.Position) ([]protocol.CompletionItem, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, nil
	}

	found, prefix := a.isInServiceIDAttribute(pos)
	if !found {
		return nil, nil
	}

	return a.serviceCompletionItems(prefix), nil
}

func (a *xmlAnalyzer) serviceCompletionItems(prefix string) []protocol.CompletionItem {
	items := []protocol.CompletionItem{}
	seen := make(map[string]bool)
	kind := protocol.CompletionItemKindKeyword

	for id, class := range a.container.ServiceClasses {
		if !strings.HasPrefix(id, ".") && strings.Contains(id, prefix) {
			if _, ok := seen[id]; !ok {
				item := protocol.CompletionItem{
					Label:  id,
					Kind:   &kind,
					Detail: &class,
				}
				items = append(items, item)
				seen[id] = true
			}
		}
	}

	for alias, serviceId := range a.container.ServiceAliases {
		if !strings.HasPrefix(alias, ".") && strings.Contains(alias, prefix) {
			if _, ok := seen[alias]; !ok {
				detail := "alias for " + serviceId
				item := protocol.CompletionItem{
					Label:  alias,
					Kind:   &kind,
					Detail: &detail,
				}
				items = append(items, item)
				seen[alias] = true
			}
		}
	}

	sort.Slice(items, func(i, j int) bool {
		idI := items[i].Label
		idJ := items[j].Label
		refI := a.container.ServiceReferences[idI]
		refJ := a.container.ServiceReferences[idJ]

		if refI != refJ {
			return refI > refJ
		}
		return idI < idJ
	})

	return items
}

func (a *xmlAnalyzer) OnDefinition(pos protocol.Position) ([]protocol.Location, error) {
	a.mu.RLock()
	content := string(a.content)
	container := a.container
	autoload := a.autoload
	a.mu.RUnlock()

	if container == nil {
		return nil, nil
	}

	if twigPath, ok := twig.PathAt(content, pos); ok {
		if target, ok := twig.Resolve(twigPath, container); ok {
			loc := protocol.Location{
				URI:   protocol.DocumentUri(utils.PathToURI(target)),
				Range: protocol.Range{},
			}
			return []protocol.Location{loc}, nil
		}
	}

	line, ok := lineAt(content, int(pos.Line))
	if !ok || line == "" {
		return nil, nil
	}

	identifier, _, _, ok := extractIdentifier(line, int(pos.Character), isServiceIdentifierWithAtRune)
	if !ok {
		return nil, nil
	}
	identifier = trimQuotes(strings.TrimSpace(identifier))
	if identifier == "" {
		return nil, nil
	}

	if strings.HasPrefix(identifier, "@") {
		if locs, ok := resolveServiceIDLocations(strings.TrimPrefix(identifier, "@"), container, autoload); ok {
			return locs, nil
		}
		identifier = strings.TrimPrefix(identifier, "@")
	}

	if strings.Contains(identifier, "\\") {
		if locs, ok := resolveClassLocations(identifier, container, autoload); ok {
			return locs, nil
		}
	}

	if locs, ok := resolveServiceIDLocations(identifier, container, autoload); ok {
		return locs, nil
	}

	return nil, nil
}
