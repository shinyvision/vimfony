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
	routes            config.RoutesMap
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

			if start >= 0 && end <= len(a.content) {
				content := strings.TrimSpace(string(a.content[start:end]))
				switch captureName {
				case "assignedVariable":
					variableName = content
				case "assignedValue":
					assignedValue = content
				}
			}
		}

		if variableName != "" && assignedValue != "" {
			variables[variableName] = assignedValue
		} else if _, ok := variables[variableName]; !ok && variableName != "" {
			valueless = append(valueless, variableName)
		}
	}

	return variables, valueless
}

func (a *twigAnalyzer) SetContainerConfig(container *config.ContainerConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.container = container
}

func (a *twigAnalyzer) SetRoutes(routes config.RoutesMap) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.routes = routes
}

// isTypingRouteName checks if we're typing inside the first string argument of path() or url()
func (a *twigAnalyzer) isTypingRouteName(pos protocol.Position) (bool, string) {
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

	// Walk up the tree to find if we're in a string that's the first argument of path/url
	// Tree structure: function_call -> arguments -> argument -> argument_value -> string
	for !node.IsNull() {
		if node.Type() == "string" {
			// Walk up to find the argument
			argValue := node.Parent()
			if !argValue.IsNull() && argValue.Type() == "argument_value" {
				arg := argValue.Parent()
				if !arg.IsNull() && arg.Type() == "argument" {
					args := arg.Parent()
					if !args.IsNull() && args.Type() == "arguments" {
						// Check if this is the first argument
						firstArg := args.NamedChild(0)
						if !firstArg.IsNull() && firstArg.Equal(arg) {
							// Get the function name
							funcNode := args.Parent()
							if !funcNode.IsNull() && funcNode.Type() == "function_call" {
								funcNameNode := funcNode.NamedChild(0) // First named child is the function name
								if !funcNameNode.IsNull() {
									funcName := string(a.content[funcNameNode.StartByte():funcNameNode.EndByte()])
									if funcName == "path" || funcName == "url" {
										// Extract the partial route name
										start := int(node.StartByte())
										end := int(node.EndByte())
										if start < len(a.content) && end <= len(a.content) {
											// The string includes quotes
											strWithQuotes := string(a.content[start:end])
											if len(strWithQuotes) < 2 {
												return false, ""
											}
											// Remove the quotes
											strContent := strWithQuotes[1 : len(strWithQuotes)-1]

											// Get the cursor position relative to the content inside quotes
											caret := lspPosToByteOffset(a.content, pos)
											if caret > start && caret < end {
												// Cursor is inside the string (between quotes)
												relCaret := caret - start - 1 // -1 for the opening quote
												if relCaret >= 0 && relCaret <= len(strContent) {
													return true, strContent[:relCaret]
												}
											}
											return true, strContent
										}
									}
								}
							}
						}
					}
				}
			}
		}
		node = node.Parent()
	}

	return false, ""
}

// isTypingRouteParameter checks if we're typing inside a hash key in the second argument of path() or url()
// Returns: (found, routeName, paramPrefix)
// Tree structure:
//   - Normal case: hash -> hash_key -> string
//   - Edge case (unborn key): hash -> hash_value -> string (when there's no hash_key yet)
func (a *twigAnalyzer) isTypingRouteParameter(pos protocol.Position) (bool, string, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tree == nil {
		return false, "", ""
	}

	point, ok := lspPosToPoint(pos, a.content)
	if !ok {
		return false, "", ""
	}

	root := a.tree.RootNode()
	if root.IsNull() {
		return false, "", ""
	}

	node := root.NamedDescendantForPointRange(point, point)

	// Walk up to find if we're in a hash key
	for !node.IsNull() {
		if node.Type() == "string" {
			parent := node.Parent()

			// Case 1: Normal hash_key case (e.g., {'key': value})
			if !parent.IsNull() && parent.Type() == "hash_key" {
				hashNode := parent.Parent()
				if !hashNode.IsNull() && hashNode.Type() == "hash" {
					found, routeName, prefix := a.extractRouteParameterInfo(node, hashNode, pos)
					if found {
						return found, routeName, prefix
					}
				}
			}

			// Case 2: Unborn hash_key case (e.g., {'key'} without colon and value)
			// In this case, tree-sitter parses 'key' as a hash_value
			if !parent.IsNull() && parent.Type() == "hash_value" {
				hashNode := parent.Parent()
				if !hashNode.IsNull() && hashNode.Type() == "hash" {
					// Check if there's no hash_key in this hash (unborn key scenario)
					hasHashKey := false
					for i := uint32(0); i < hashNode.NamedChildCount(); i++ {
						child := hashNode.NamedChild(i)
						if !child.IsNull() && child.Type() == "hash_key" {
							hasHashKey = true
							break
						}
					}

					if !hasHashKey {
						found, routeName, prefix := a.extractRouteParameterInfo(node, hashNode, pos)
						if found {
							return found, routeName, prefix
						}
					}
				}
			}
		}
		node = node.Parent()
	}

	return false, "", ""
}

// Helper function to extract route parameter information from a hash node
func (a *twigAnalyzer) extractRouteParameterInfo(stringNode, hashNode sitter.Node, pos protocol.Position) (bool, string, string) {
	// Check if this hash is inside the second argument to path/url
	argValue := hashNode.Parent()
	if !argValue.IsNull() && argValue.Type() == "argument_value" {
		arg := argValue.Parent()
		if !arg.IsNull() && arg.Type() == "argument" {
			args := arg.Parent()
			if !args.IsNull() && args.Type() == "arguments" {
				funcNode := args.Parent()
				if !funcNode.IsNull() && funcNode.Type() == "function_call" {
					funcNameNode := funcNode.NamedChild(0)
					if !funcNameNode.IsNull() {
						funcName := string(a.content[funcNameNode.StartByte():funcNameNode.EndByte()])
						if funcName == "path" || funcName == "url" {
							// Get the route name from the first argument
							firstArg := args.NamedChild(0)
							if !firstArg.IsNull() {
								// Navigate to the string inside the first argument
								firstArgValue := firstArg.NamedChild(0)
								if !firstArgValue.IsNull() {
									firstArgString := firstArgValue.NamedChild(0)
									if !firstArgString.IsNull() && firstArgString.Type() == "string" {
										routeNameWithQuotes := string(a.content[firstArgString.StartByte():firstArgString.EndByte()])
										if len(routeNameWithQuotes) < 2 {
											return false, "", ""
										}
										routeName := routeNameWithQuotes[1 : len(routeNameWithQuotes)-1]

										// Get the partial parameter key
										start := int(stringNode.StartByte())
										end := int(stringNode.EndByte())
										if start < len(a.content) && end <= len(a.content) {
											keyWithQuotes := string(a.content[start:end])
											if len(keyWithQuotes) < 2 {
												return false, "", ""
											}
											keyContent := keyWithQuotes[1 : len(keyWithQuotes)-1]

											caret := lspPosToByteOffset(a.content, pos)
											if caret > start && caret < end {
												// Cursor is inside the string (between quotes)
												relCaret := caret - start - 1 // -1 for the opening quote
												if relCaret >= 0 && relCaret <= len(keyContent) {
													return true, routeName, keyContent[:relCaret]
												}
											}
											return true, routeName, keyContent
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return false, "", ""
}

func (a *twigAnalyzer) OnCompletion(pos protocol.Position) ([]protocol.CompletionItem, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.container == nil {
		return nil, nil
	}

	var items []protocol.CompletionItem

	// Check for route name completion
	if foundRoute, routePrefix := a.isTypingRouteName(pos); foundRoute {
		routeItems := a.routeNameCompletionItems(routePrefix)
		items = append(items, routeItems...)
	}

	// Check for route parameter completion
	if foundParam, routeName, paramPrefix := a.isTypingRouteParameter(pos); foundParam {
		paramItems := a.routeParameterCompletionItems(routeName, paramPrefix)
		items = append(items, paramItems...)
	}

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

	definedVariables, capturedVariables := a.getDefinedVariables()

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
	for _, variable := range capturedVariables {
		if strings.HasPrefix(variable, prefix) {
			item := protocol.CompletionItem{
				Label: variable,
				Kind:  &kind,
			}
			items = append(items, item)
		}
	}

	return items
}

func (a *twigAnalyzer) routeNameCompletionItems(prefix string) []protocol.CompletionItem {
	items := []protocol.CompletionItem{}
	kind := protocol.CompletionItemKindConstant

	for name, route := range a.routes {
		if strings.HasPrefix(name, prefix) {
			detail := "Symfony route"

			// Build documentation showing route name and parameters
			var docBuilder strings.Builder
			docBuilder.WriteString("**Route:** `")
			docBuilder.WriteString(name)
			docBuilder.WriteString("`\n\n")

			if len(route.Parameters) > 0 {
				docBuilder.WriteString("**Parameters:**\n")
				for _, param := range route.Parameters {
					docBuilder.WriteString("- `")
					docBuilder.WriteString(param)
					docBuilder.WriteString("`\n")
				}
			} else {
				docBuilder.WriteString("*No parameters*")
			}

			documentation := protocol.MarkupContent{
				Kind:  protocol.MarkupKindMarkdown,
				Value: docBuilder.String(),
			}

			item := protocol.CompletionItem{
				Label:         name,
				Kind:          &kind,
				Detail:        &detail,
				Documentation: documentation,
			}
			items = append(items, item)
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Label < items[j].Label
	})

	return items
}

func (a *twigAnalyzer) routeParameterCompletionItems(routeName, prefix string) []protocol.CompletionItem {
	items := []protocol.CompletionItem{}
	kind := protocol.CompletionItemKindProperty

	route, ok := a.routes[routeName]
	if !ok {
		return items
	}

	for _, param := range route.Parameters {
		if strings.HasPrefix(param, prefix) {
			detail := fmt.Sprintf("parameter for route %s", routeName)
			item := protocol.CompletionItem{
				Label:  param,
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
