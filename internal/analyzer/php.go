package analyzer

import (
	"context"
	"regexp"
	"sync"

	php "github.com/alexaandru/go-sitter-forest/php"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type PhpAnalyzer interface {
	IsInAutoconfigure(pos protocol.Position) (bool, string)
}

type phpAnalyzer struct {
	parser         *sitter.Parser
	mu             sync.RWMutex
	attributeQuery *sitter.Query
	servicesRe     *regexp.Regexp
	tree           *sitter.Tree
	content        []byte
}

func NewPHPAnalyzer() Analyzer {
	p := sitter.NewParser()
	lang := sitter.NewLanguage(php.GetLanguage())
	_ = p.SetLanguage(lang)
	attributeQuery, _ := sitter.NewQuery(lang, []byte(`
      (attribute
        [(qualified_name) (name)] @name
      ) @attr
    `))
	servicesRe := regexp.MustCompile(`['"\\](@?[A-Za-z0-9_.\\-]*)$`)
	return &phpAnalyzer{
		parser:         p,
		attributeQuery: attributeQuery,
		servicesRe:     servicesRe,
	}
}

func (a *phpAnalyzer) Changed(code []byte, change *sitter.InputEdit) error {
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

func (a *phpAnalyzer) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tree != nil {
		a.tree.Close()
		a.tree = nil
	}
}

func (a *phpAnalyzer) IsInAutoconfigure(pos protocol.Position) (bool, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tree == nil || a.attributeQuery == nil {
		return false, ""
	}

	point, ok := lspPosToPoint(pos, a.content)
	if !ok {
		return false, ""
	}

	root := a.tree.RootNode()
	q := a.attributeQuery
	qc := sitter.NewQueryCursor()
	it := qc.Matches(q, root, a.content)

	for {
		m := it.Next()
		if m == nil {
			break
		}

		var nameNode, attrNode *sitter.Node
		for _, c := range m.Captures {
			switch q.CaptureNameForID(c.Index) {
			case "name":
				nameNode = &c.Node
			case "attr":
				attrNode = &c.Node
			}
		}
		if nameNode == nil || attrNode == nil {
			continue
		}
		if shortName(nameNode.Content(a.content)) != "Autoconfigure" {
			continue
		}
		sp, ep := attrNode.StartPoint(), attrNode.EndPoint()
		if !(sp.Row <= point.Row && point.Row <= ep.Row) {
			continue
		}

		node := root.NamedDescendantForPointRange(point, point)
		if node.IsNull() {
			continue
		}
		t := node.Type()
		if t != "string" && t != "string_content" {
			continue
		}

		lineUntilCaret := linePrefixAtPoint(a.content, point)
		if m := a.servicesRe.FindSubmatch(lineUntilCaret); len(m) > 1 {
			return true, string(m[1])
		}
		return true, ""
	}

	return false, ""
}
