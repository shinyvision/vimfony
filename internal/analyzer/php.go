package analyzer

import (
	"context"
	"regexp"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/php"
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
	p.SetLanguage(php.GetLanguage())

	// We precompile our regexps + tree-sitter queries. Normally no big deal, but we query often.
	attributeQuery, _ := sitter.NewQuery([]byte(`
      (attribute
        [(qualified_name) (name)] @name
      ) @attr
    `), php.GetLanguage())
	servicesRe := regexp.MustCompile(`['"\\](@?[A-Za-z0-9_.\\-]*)$`)

	return &phpAnalyzer{
		parser:         p,
		attributeQuery: attributeQuery,
		servicesRe:     servicesRe,
	}
}

// It gets called from the document: code has changed; compute changes incrementally using our old tree
func (a *phpAnalyzer) Changed(code []byte, change *sitter.EditInput) error {
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
		a.tree.Close() // Always close old tree
	}
	a.tree = newTree
	return nil
}

// Document closed: close tree
func (a *phpAnalyzer) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tree != nil {
		a.tree.Close()
		a.tree = nil
	}
}

// We check using the tree-sitter if we are in an #[Autoconfigure] attribute
func (a *phpAnalyzer) IsInAutoconfigure(pos protocol.Position) (bool, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tree == nil {
		return false, "" // No tree, no analyze
	}

	root := a.tree.RootNode()
	q := a.attributeQuery
	if q == nil {
		return false, ""
	}

	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(q, root)

	point, ok := lspPosToPoint(pos, a.content)
	if !ok {
		return false, ""
	}

	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}

		var nameNode, attrNode *sitter.Node
		for _, c := range m.Captures {
			switch q.CaptureNameForId(c.Index) {
			case "name":
				nameNode = c.Node
			case "attr":
				attrNode = c.Node
			}
		}

		if shortName(nameNode.Content(a.content)) != "Autoconfigure" {
			continue
		}

		if !(attrNode.StartPoint().Row <= point.Row && point.Row <= attrNode.EndPoint().Row) {
			continue
		}

		node := root.NamedDescendantForPointRange(point, point)
		if node == nil {
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
