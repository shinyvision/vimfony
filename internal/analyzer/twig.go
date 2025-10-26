package analyzer

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	twig "github.com/alexaandru/go-sitter-forest/twig"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	twiglib "github.com/shinyvision/vimfony/internal/twig"
	"github.com/shinyvision/vimfony/internal/utils"
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
	routes            config.RoutesMap
}

type twigCallCtx struct {
	fnNode   sitter.Node
	fnName   string
	argsNode sitter.Node
	argIndex int
	strNode  sitter.Node
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
	  (for_statement
	    (repeat) @repeat
	    (variable) @assignedVariable
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
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tree == nil || a.variableLikeQuery == nil {
		return false, ""
	}

	caret := lspPosToByteOffset(a.content, pos)
	if caret < 0 {
		return false, ""
	}

	root := a.tree.RootNode()
	qc := sitter.NewQueryCursor()
	it := qc.Matches(a.variableLikeQuery, root, a.content)

	for {
		m := it.Next()
		if m == nil {
			break
		}
		for _, cap := range m.Captures {
			if a.variableLikeQuery.CaptureNameForID(cap.Index) != "variableLike" {
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

func (a *twigAnalyzer) getDefinedVariables() (map[string]string, []string) {
	if a.tree == nil || a.assignmentQuery == nil {
		return nil, nil
	}

	variables := make(map[string]string)
	var valueless []string

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
			if start < 0 || end > len(a.content) {
				continue
			}
			content := strings.TrimSpace(string(a.content[start:end]))
			switch captureName {
			case "assignedVariable":
				variableName = content
			case "assignedValue":
				assignedValue = content
			}
		}

		if variableName != "" && assignedValue != "" {
			variables[variableName] = assignedValue
		} else if variableName != "" {
			if _, ok := variables[variableName]; !ok {
				valueless = append(valueless, variableName)
			}
		}
	}

	// Fallback: catch {% set name = ... %} where RHS is not a variable node (e.g. string/number)
	setRe := regexp.MustCompile(`\{\%\s*set\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	for _, m := range setRe.FindAllSubmatch(a.content, -1) {
		name := string(m[1])
		if name == "" {
			continue
		}
		if _, ok := variables[name]; ok {
			continue
		}
		found := false
		for _, v := range valueless {
			if v == name {
				found = true
				break
			}
		}
		if !found {
			valueless = append(valueless, name)
		}
	}

	return variables, valueless
}

func (a *twigAnalyzer) SetContainerConfig(container *config.ContainerConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.container = container
}

func (a *twigAnalyzer) SetRoutes(routes *config.RoutesMap) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if routes == nil {
		a.routes = nil
		return
	}
	a.routes = *routes
}

func (a *twigAnalyzer) OnDefinition(pos protocol.Position) ([]protocol.Location, error) {
	a.mu.RLock()
	content := string(a.content)
	container := a.container
	a.mu.RUnlock()

	if container == nil {
		return nil, nil
	}

	if twigPath, ok := twiglib.PathAt(content, pos); ok {
		if target, ok := twiglib.Resolve(twigPath, container); ok {
			loc := protocol.Location{
				URI:   protocol.DocumentUri(utils.PathToURI(target)),
				Range: protocol.Range{},
			}
			return []protocol.Location{loc}, nil
		}
	}

	if functionName, ok := twiglib.FunctionAt(content, pos); ok {
		if loc, ok := container.TwigFunctions[functionName]; ok {
			return []protocol.Location{loc}, nil
		}
	}

	return nil, nil
}

func (a *twigAnalyzer) routeContextAt(pos protocol.Position) (twigCallCtx, bool) {
	if a.tree == nil {
		return twigCallCtx{}, false
	}

	point, ok := lspPosToPoint(pos, a.content)
	if !ok {
		return twigCallCtx{}, false
	}
	root := a.tree.RootNode()
	if root.IsNull() {
		return twigCallCtx{}, false
	}

	n := root.NamedDescendantForPointRange(point, point)
	var str sitter.Node
	for nn := n; !nn.IsNull(); nn = nn.Parent() {
		if str.IsNull() && nn.Type() == "string" {
			str = nn
		}
		if nn.Type() == "function_call" {
			nameNode := nn.NamedChild(0)
			if nameNode.IsNull() {
				return twigCallCtx{}, false
			}
			fnName := string(a.content[nameNode.StartByte():nameNode.EndByte()])
			if fnName != "path" && fnName != "url" {
				return twigCallCtx{}, false
			}
			args := nn.NamedChild(1)
			if args.IsNull() || args.Type() != "arguments" {
				return twigCallCtx{}, false
			}

			argIdx := -1
			for p := str; !p.IsNull(); p = p.Parent() {
				if p.Type() == "argument" {
					for i := uint32(0); i < args.NamedChildCount(); i++ {
						if args.NamedChild(i).Equal(p) {
							argIdx = int(i)
							break
						}
					}
					break
				}
				if p.Equal(nn) {
					break
				}
			}
			if argIdx < 0 {
				return twigCallCtx{}, false
			}
			return twigCallCtx{
				fnNode:   nn,
				fnName:   fnName,
				argsNode: args,
				argIndex: argIdx,
				strNode:  str,
			}, true
		}
	}
	return twigCallCtx{}, false
}

func (a *twigAnalyzer) stringPrefix(str sitter.Node, pos protocol.Position) string {
	if str.IsNull() {
		return ""
	}
	sb, eb := int(str.StartByte()), int(str.EndByte())
	if eb-sb < 2 {
		return ""
	}
	inner := a.content[sb+1 : eb-1]

	caret := lspPosToByteOffset(a.content, pos)
	if caret > sb && caret < eb {
		rel := caret - sb - 1
		if rel >= 0 && rel <= len(inner) {
			return string(inner[:rel])
		}
	}
	return string(inner)
}

func (a *twigAnalyzer) firstArgRouteName(args sitter.Node) string {
	if args.IsNull() || args.Type() != "arguments" {
		return ""
	}
	first := args.NamedChild(0)
	if first.IsNull() {
		return ""
	}
	av := first.NamedChild(0)
	if av.IsNull() {
		return ""
	}
	str := av.NamedChild(0)
	if str.IsNull() || str.Type() != "string" {
		return ""
	}
	sb, eb := int(str.StartByte()), int(str.EndByte())
	if eb-sb < 2 {
		return ""
	}
	return string(a.content[sb+1 : eb-1])
}

func hasAnyHashKey(hashNode sitter.Node) bool {
	for i := uint32(0); i < hashNode.NamedChildCount(); i++ {
		if !hashNode.NamedChild(i).IsNull() && hashNode.NamedChild(i).Type() == "hash_key" {
			return true
		}
	}
	return false
}

// key context only: hash_key always, hash_value only when no hash_key exists yet
func isParamKeyContext(strNode sitter.Node) bool {
	p := strNode.Parent()
	if p.IsNull() {
		return false
	}
	switch p.Type() {
	case "hash_key":
		return true
	case "hash_value":
		hash := p.Parent()
		if hash.IsNull() || hash.Type() != "hash" {
			return false
		}
		return !hasAnyHashKey(hash)
	default:
		return false
	}
}

func (a *twigAnalyzer) isTypingRouteName(pos protocol.Position) (bool, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	ctx, ok := a.routeContextAt(pos)
	if !ok || ctx.argIndex != 0 {
		return false, ""
	}
	return true, a.stringPrefix(ctx.strNode, pos)
}

func (a *twigAnalyzer) isTypingRouteParameter(pos protocol.Position) (bool, string, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	ctx, ok := a.routeContextAt(pos)
	if !ok || ctx.argIndex != 1 || !isParamKeyContext(ctx.strNode) {
		return false, "", ""
	}
	routeName := a.firstArgRouteName(ctx.argsNode)
	if routeName == "" {
		return false, "", ""
	}
	return true, routeName, a.stringPrefix(ctx.strNode, pos)
}

func (a *twigAnalyzer) OnCompletion(pos protocol.Position) ([]protocol.CompletionItem, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, nil
	}

	var items []protocol.CompletionItem

	items = append(items, a.routeNameCompletionItems(pos)...)
	items = append(items, a.routeParameterCompletionItems(pos)...)

	if foundFunction, functionPrefix := a.isTypingFunction(pos); foundFunction {
		items = append(items, a.twigFunctionCompletionItems(functionPrefix)...)
	}
	if foundVariable, variablePrefix := a.isTypingVariable(pos); foundVariable {
		items = append(items, a.twigVariableCompletionItems(variablePrefix)...)
	}

	if len(items) == 0 {
		return nil, nil
	}

	sort.Slice(items, func(i, j int) bool {
		li, lj := items[i].Label, items[j].Label
		if len(li) != len(lj) {
			return len(li) < len(lj)
		}
		return li < lj
	})

	return items, nil
}

func (a *twigAnalyzer) twigFunctionCompletionItems(prefix string) []protocol.CompletionItem {
	items := []protocol.CompletionItem{}
	kind := protocol.CompletionItemKindFunction

	for name := range a.container.TwigFunctions {
		if strings.HasPrefix(name, prefix) {
			detail := fmt.Sprintf("%s twig function", name)
			items = append(items, protocol.CompletionItem{
				Label:  name,
				Kind:   &kind,
				Detail: &detail,
			})
		}
	}
	return items
}

func (a *twigAnalyzer) twigVariableCompletionItems(prefix string) []protocol.CompletionItem {
	items := []protocol.CompletionItem{}
	kind := protocol.CompletionItemKindVariable

	definedVariables, capturedVariables := a.getDefinedVariables()

	for variable, value := range definedVariables {
		if strings.HasPrefix(variable, prefix) {
			detail := fmt.Sprintf("{%% set %s = %s %%}", variable, value)
			items = append(items, protocol.CompletionItem{
				Label:  variable,
				Kind:   &kind,
				Detail: &detail,
			})
		}
	}
	for _, variable := range capturedVariables {
		if strings.HasPrefix(variable, prefix) {
			items = append(items, protocol.CompletionItem{
				Label: variable,
				Kind:  &kind,
			})
		}
	}
	return items
}

func (a *twigAnalyzer) routeNameCompletionItems(pos protocol.Position) []protocol.CompletionItem {
	found, prefix := a.isTypingRouteName(pos)
	if !found {
		return nil
	}
	return makeRouteNameCompletionItems(a.routes, prefix)
}

func (a *twigAnalyzer) routeParameterCompletionItems(pos protocol.Position) []protocol.CompletionItem {
	found, routeName, prefix := a.isTypingRouteParameter(pos)
	if !found {
		return nil
	}
	return makeRouteParameterCompletionItems(a.routes, routeName, prefix)
}
