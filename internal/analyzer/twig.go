package analyzer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	twig "github.com/alexaandru/go-sitter-forest/twig"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type TwigAnalyzer interface {
	ContainerAware
	IsTypingFunction(pos protocol.Position) (bool, string)
}

type twigAnalyzer struct {
	parser     *sitter.Parser
	mu         sync.RWMutex
	tree       *sitter.Tree
	content    []byte
	identQuery *sitter.Query
	container  *config.ContainerConfig
}

func NewTwigAnalyzer() Analyzer {
	p := sitter.NewParser()
	lang := sitter.NewLanguage(twig.GetLanguage())
	_ = p.SetLanguage(lang)
	q, _ := sitter.NewQuery(lang, []byte(`
	  (variable) @ident
	  (function_identifier) @ident
	`))
	return &twigAnalyzer{parser: p, identQuery: q}
}

func (a *twigAnalyzer) Changed(code []byte, change *sitter.InputEdit) error {
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

func (a *twigAnalyzer) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tree != nil {
		a.tree.Close()
		a.tree = nil
	}
}

// IsTypingFunction checks if we're typing a function identifier or a variable since TS twig variables kinda look like functions
func (a *twigAnalyzer) IsTypingFunction(pos protocol.Position) (bool, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tree == nil || a.identQuery == nil {
		return false, ""
	}

	caret := lspPosToByteOffset(a.content, pos)
	if caret < 0 {
		return false, ""
	}

	root := a.tree.RootNode()
	qc := sitter.NewQueryCursor()
	it := qc.Matches(a.identQuery, root, a.content)

	for {
		m := it.Next()
		if m == nil {
			break
		}
		for _, cap := range m.Captures {
			if a.identQuery.CaptureNameForID(cap.Index) != "ident" {
				continue
			}
			n := cap.Node
			start := int(n.StartByte())
			end := int(n.EndByte())
			if caret < start || caret > end {
				continue
			}
			return true, string(a.content[start:caret])
		}
	}
	return false, ""
}

func (a *twigAnalyzer) SetContainerConfig(container *config.ContainerConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.container = container
}

func (a *twigAnalyzer) OnCompletion(pos protocol.Position) ([]protocol.CompletionItem, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, nil
	}

	found, prefix := a.IsTypingFunction(pos)
	if !found {
		return nil, nil
	}

	return a.twigFunctionCompletionItems(prefix), nil
}

func (a *twigAnalyzer) twigFunctionCompletionItems(prefix string) []protocol.CompletionItem {
	items := []protocol.CompletionItem{}
	kind := protocol.CompletionItemKindFunction

	for name := range a.container.TwigFunctions {
		if strings.HasPrefix(name, prefix) {
			detail := fmt.Sprintf("%s twig function", name)
			item := protocol.CompletionItem{
				Label:  name,
				Kind:   &kind,
				Detail: &detail,
			}
			items = append(items, item)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Label < items[j].Label
	})

	return items
}
