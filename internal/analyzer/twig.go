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

type twigAnalyzer struct {
	parser            *sitter.Parser
	mu                sync.RWMutex
	tree              *sitter.Tree
	content           []byte
	functionLikeQuery *sitter.Query
	variableLikeQuery *sitter.Query
	assignmentQuery   *sitter.Query
	container         *config.ContainerConfig
}

func NewTwigAnalyzer() Analyzer {
	p := sitter.NewParser()
	lang := sitter.NewLanguage(twig.GetLanguage())
	_ = p.SetLanguage(lang)

	functionLikeQuery, _ := sitter.NewQuery(lang, []byte(`
	  (variable) @functionLike
	  (function_identifier) @functionLike
	`))

	variableLikeQuery, _ := sitter.NewQuery(lang, []byte(`
	  (variable) @variableLike
	`))

	assignmentQuery, _ := sitter.NewQuery(lang, []byte(`
	  (assignment_statement
	    (keyword) @keyword (#eq? @keyword "set")
	    (variable) @assignedVariable
	    (variable) @assignedValue
	  )
	`))

	return &twigAnalyzer{
		parser:            p,
		functionLikeQuery: functionLikeQuery,
		variableLikeQuery: variableLikeQuery,
		assignmentQuery:   assignmentQuery,
	}
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

// isTypingFunction checks if we're typing a function identifier or a variable since TS twig variables kinda look like functions
func (a *twigAnalyzer) isTypingFunction(pos protocol.Position) (bool, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tree == nil || a.functionLikeQuery == nil {
		return false, ""
	}

	caret := lspPosToByteOffset(a.content, pos)
	if caret < 0 {
		return false, ""
	}

	root := a.tree.RootNode()
	qc := sitter.NewQueryCursor()
	it := qc.Matches(a.functionLikeQuery, root, a.content)

	for {
		m := it.Next()
		if m == nil {
			break
		}
		for _, cap := range m.Captures {
			if a.functionLikeQuery.CaptureNameForID(cap.Index) != "functionLike" {
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

func (a *twigAnalyzer) isTypingVariable(pos protocol.Position) (bool, string) {
	return a.isTypingFunction(pos) // Variables and functions use the same detection logic in Twig
}

// getDefinedVariables extracts all variables defined in the current template using {% set %} statements
func (a *twigAnalyzer) getDefinedVariables() map[string]string {
	if a.tree == nil || a.assignmentQuery == nil {
		return nil
	}

	variables := make(map[string]string)

	root := a.tree.RootNode()
	qc := sitter.NewQueryCursor()
	it := qc.Matches(a.assignmentQuery, root, a.content)

	for {
		m := it.Next()
		if m == nil {
			break
		}

		var variableName, assignedValue string
		for _, cap := range m.Captures {
			captureName := a.assignmentQuery.CaptureNameForID(cap.Index)
			n := cap.Node
			start := int(n.StartByte())
			end := int(n.EndByte())

			if start >= 0 && end <= len(a.content) {
				content := strings.TrimSpace(string(a.content[start:end]))
				if captureName == "assignedVariable" {
					variableName = content
				} else if captureName == "assignedValue" {
					assignedValue = content
				}
			}
		}

		if variableName != "" && assignedValue != "" {
			variables[variableName] = assignedValue
		}
	}

	return variables
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

	var items []protocol.CompletionItem

	if foundFunction, functionPrefix := a.isTypingFunction(pos); foundFunction {
		functionItems := a.twigFunctionCompletionItems(functionPrefix)
		items = append(items, functionItems...)
	}

	if foundVariable, variablePrefix := a.isTypingVariable(pos); foundVariable {
		variableItems := a.twigVariableCompletionItems(variablePrefix)
		items = append(items, variableItems...)
	}

	if len(items) == 0 {
		return nil, nil
	}

	sort.Slice(items, func(i, j int) bool {
		labelI := items[i].Label
		labelJ := items[j].Label
		if len(labelI) != len(labelJ) {
			return len(labelI) < len(labelJ)
		}
		return labelI < labelJ
	})

	return items, nil
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

func (a *twigAnalyzer) twigVariableCompletionItems(prefix string) []protocol.CompletionItem {
	items := []protocol.CompletionItem{}
	kind := protocol.CompletionItemKindVariable

	definedVariables := a.getDefinedVariables()

	for variable, value := range definedVariables {
		if strings.HasPrefix(variable, prefix) {
			detail := fmt.Sprintf("{%% set %s = %s %%}", variable, value)
			item := protocol.CompletionItem{
				Label:  variable,
				Kind:   &kind,
				Detail: &detail,
			}
			items = append(items, item)
		}
	}

	return items
}
