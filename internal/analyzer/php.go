package analyzer

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	php "github.com/alexaandru/go-sitter-forest/php"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type phpAnalyzer struct {
	parser         *sitter.Parser
	mu             sync.RWMutex
	attributeQuery *sitter.Query
	servicesRe     *regexp.Regexp
	tree           *sitter.Tree
	content        []byte
	container      *config.ContainerConfig
	routes         config.RoutesMap
	propertyTypes  map[string][]typeOccurrence
	functionVars   map[string]map[string][]typeOccurrence
}

type typeOccurrence struct {
	Type string
	Line int
}

type phpRouteCallCtx struct {
	callNode sitter.Node
	argsNode sitter.Node
	argIndex int
	strNode  sitter.Node
	property string
	variable string
}

const (
	urlGeneratorInterfaceFQN = "Symfony\\Component\\Routing\\Generator\\UrlGeneratorInterface"
	urlGeneratorFQN          = "Symfony\\Component\\Routing\\Generator\\UrlGenerator"
	routerInterfaceFQN       = "Symfony\\Component\\Routing\\RouterInterface"
	routerFQN                = "Symfony\\Component\\Routing\\Router"
)

var routerCanonical = func() map[string]string {
	c := map[string]string{}
	fqn := []string{
		urlGeneratorInterfaceFQN,
		urlGeneratorFQN,
		routerInterfaceFQN,
		routerFQN,
	}
	for _, x := range fqn {
		c[strings.ToLower(x)] = x
		c[strings.ToLower(shortName(x))] = x
	}
	return c
}()

var docblockVarRe = regexp.MustCompile(`@var\s+([^\s]+)\s+\$([A-Za-z_][A-Za-z0-9_]*)`)

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
	a.propertyTypes = a.collectPropertyTypes()
	a.functionVars = a.collectFunctionVariableTypes()
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

func (a *phpAnalyzer) isInAutoconfigure(pos protocol.Position) (bool, string) {
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

func (a *phpAnalyzer) SetContainerConfig(container *config.ContainerConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.container = container
}

func (a *phpAnalyzer) SetRoutes(routes config.RoutesMap) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.routes = routes
}

func (a *phpAnalyzer) OnCompletion(pos protocol.Position) ([]protocol.CompletionItem, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	items := make([]protocol.CompletionItem, 0)

	if a.container != nil {
		if found, prefix := a.isInAutoconfigure(pos); found && strings.HasPrefix(prefix, "@") {
			servicePrefix := strings.TrimPrefix(prefix, "@")
			items = append(items, a.serviceCompletionItems(servicePrefix)...)
		}
	}

	if len(a.routes) > 0 {
		items = append(items, a.phpRouteNameCompletionItems(pos)...)
		items = append(items, a.phpRouteParameterCompletionItems(pos)...)
	}

	if len(items) == 0 {
		return nil, nil
	}

	return items, nil
}

func (a *phpAnalyzer) serviceCompletionItems(prefix string) []protocol.CompletionItem {
	items := []protocol.CompletionItem{}
	seen := map[string]struct{}{}
	kind := protocol.CompletionItemKindKeyword

	add := func(label, detail string) {
		if strings.HasPrefix(label, ".") || !strings.Contains(label, prefix) {
			return
		}
		if _, ok := seen[label]; ok {
			return
		}
		items = append(items, protocol.CompletionItem{
			Label:  label,
			Kind:   &kind,
			Detail: &detail,
		})
		seen[label] = struct{}{}
	}

	for id, class := range a.container.ServiceClasses {
		add(id, class)
	}
	for alias, serviceId := range a.container.ServiceAliases {
		add(alias, "alias for "+serviceId)
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

func (a *phpAnalyzer) phpRouteNameCompletionItems(pos protocol.Position) []protocol.CompletionItem {
	found, prefix := a.isTypingPhpRouteName(pos)
	if !found {
		return nil
	}
	return makeRouteNameCompletionItems(a.routes, prefix)
}

func (a *phpAnalyzer) phpRouteParameterCompletionItems(pos protocol.Position) []protocol.CompletionItem {
	found, routeName, prefix := a.isTypingPhpRouteParameter(pos)
	if !found {
		return nil
	}
	return makeRouteParameterCompletionItems(a.routes, routeName, prefix)
}

func (a *phpAnalyzer) isTypingPhpRouteName(pos protocol.Position) (bool, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	ctx, ok := a.phpRouteContextAt(pos)
	if !ok || ctx.argIndex != 0 {
		return false, ""
	}
	return true, a.stringPrefix(ctx.strNode, pos)
}

func (a *phpAnalyzer) isTypingPhpRouteParameter(pos protocol.Position) (bool, string, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	ctx, ok := a.phpRouteContextAt(pos)
	if !ok || ctx.argIndex != 1 || !a.isPHPParamKeyContext(ctx.strNode) {
		return false, "", ""
	}

	routeName := a.phpRouteNameFromArgs(ctx.argsNode)
	if routeName == "" {
		return false, "", ""
	}

	return true, routeName, a.stringPrefix(ctx.strNode, pos)
}

func (a *phpAnalyzer) phpRouteContextAt(pos protocol.Position) (phpRouteCallCtx, bool) {
	if a.tree == nil {
		return phpRouteCallCtx{}, false
	}

	point, ok := lspPosToPoint(pos, a.content)
	if !ok {
		return phpRouteCallCtx{}, false
	}

	root := a.tree.RootNode()
	if root.IsNull() {
		return phpRouteCallCtx{}, false
	}

	node := root.NamedDescendantForPointRange(point, point)
	if node.IsNull() {
		return phpRouteCallCtx{}, false
	}

	var str sitter.Node
	for cur := node; !cur.IsNull(); cur = cur.Parent() {
		if str.IsNull() {
			switch cur.Type() {
			case "string":
				str = cur
			case "string_content":
				parent := cur.Parent()
				if !parent.IsNull() && parent.Type() == "string" {
					str = parent
				}
			}
		}

		if cur.Type() != "argument" {
			continue
		}

		argNode := cur
		argsNode := argNode.Parent()
		if argsNode.IsNull() || argsNode.Type() != "arguments" {
			return phpRouteCallCtx{}, false
		}

		argIndex := -1
		for i := uint32(0); i < argsNode.NamedChildCount(); i++ {
			if argsNode.NamedChild(i).Equal(argNode) {
				argIndex = int(i)
				break
			}
		}
		if argIndex < 0 {
			return phpRouteCallCtx{}, false
		}

		callNode := argsNode.Parent()
		for !callNode.IsNull() && callNode.Type() != "member_call_expression" {
			callNode = callNode.Parent()
		}
		if callNode.IsNull() || callNode.Type() != "member_call_expression" {
			return phpRouteCallCtx{}, false
		}

		nameNode := callNode.ChildByFieldName("name")
		if nameNode.IsNull() || strings.TrimSpace(nameNode.Content(a.content)) != "generate" {
			return phpRouteCallCtx{}, false
		}

		objectNode := callNode.ChildByFieldName("object")
		if objectNode.IsNull() {
			return phpRouteCallCtx{}, false
		}

		callLine := int(callNode.StartPoint().Row) + 1
		funcName := a.enclosingFunctionName(callNode)

		propertyName := a.routerPropertyNameFromMemberAccess(objectNode)
		if propertyName != "" {
			if !a.propertyHasRouterType(propertyName) {
				return phpRouteCallCtx{}, false
			}
			if str.IsNull() {
				return phpRouteCallCtx{}, false
			}
			return phpRouteCallCtx{
				callNode: callNode,
				argsNode: argsNode,
				argIndex: argIndex,
				strNode:  str,
				property: propertyName,
			}, true
		}

		if objectNode.Type() == "variable_name" {
			varName := a.variableNameFromNode(objectNode)
			if varName == "" {
				return phpRouteCallCtx{}, false
			}
			if !a.variableHasRouterType(funcName, varName, callLine) {
				return phpRouteCallCtx{}, false
			}
			if str.IsNull() {
				return phpRouteCallCtx{}, false
			}
			return phpRouteCallCtx{
				callNode: callNode,
				argsNode: argsNode,
				argIndex: argIndex,
				strNode:  str,
				variable: varName,
			}, true
		}

		return phpRouteCallCtx{}, false
	}

	return phpRouteCallCtx{}, false
}

func (a *phpAnalyzer) phpRouteNameFromArgs(args sitter.Node) string {
	if args.IsNull() || args.Type() != "arguments" || args.NamedChildCount() == 0 {
		return ""
	}

	first := args.NamedChild(0)
	if first.IsNull() {
		return ""
	}

	value := first.ChildByFieldName("value")
	if value.IsNull() && first.NamedChildCount() > 0 {
		value = first.NamedChild(0)
	}
	if value.IsNull() {
		return ""
	}

	return a.stringContent(value)
}

func (a *phpAnalyzer) asStringNode(n sitter.Node) sitter.Node {
	if n.IsNull() {
		return n
	}
	if n.Type() == "string_content" {
		n = n.Parent()
	}
	if n.IsNull() || n.Type() != "string" {
		return sitter.Node{}
	}
	return n
}

func (a *phpAnalyzer) stringInnerBounds(n sitter.Node) (start, end int, ok bool) {
	n = a.asStringNode(n)
	if n.IsNull() {
		return 0, 0, false
	}
	sb, eb := int(n.StartByte()), int(n.EndByte())
	if eb-sb < 2 {
		return 0, 0, false
	}
	return sb + 1, eb - 1, true
}

func (a *phpAnalyzer) stringPrefix(str sitter.Node, pos protocol.Position) string {
	str = a.asStringNode(str)
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

func (a *phpAnalyzer) stringContent(str sitter.Node) string {
	s, e, ok := a.stringInnerBounds(str)
	if !ok {
		return ""
	}
	return string(a.content[s:e])
}

func (a *phpAnalyzer) isPHPParamKeyContext(str sitter.Node) bool {
	if str.IsNull() {
		return false
	}
	if str.Type() == "string_content" {
		str = str.Parent()
	}
	if str.IsNull() || str.Type() != "string" {
		return false
	}

	for cur := str.Parent(); !cur.IsNull(); cur = cur.Parent() {
		if cur.Type() != "array_element_initializer" {
			continue
		}

		namedCount := cur.NamedChildCount()
		if namedCount == 0 {
			return false
		}

		for i := range namedCount {
			child := cur.NamedChild(i)
			if !child.Equal(str) {
				continue
			}
			if namedCount == 1 {
				return true
			}
			return i == 0
		}
		break
	}

	return false
}

func (a *phpAnalyzer) routerPropertyNameFromMemberAccess(node sitter.Node) string {
	if node.IsNull() {
		return ""
	}

	switch node.Type() {
	case "member_access_expression", "nullsafe_member_access_expression":
	default:
		return ""
	}

	objectNode := node.ChildByFieldName("object")
	if objectNode.IsNull() {
		return ""
	}

	switch objectNode.Type() {
	case "variable_name":
		if strings.TrimSpace(objectNode.Content(a.content)) != "$this" {
			return ""
		}
	default:
		return ""
	}

	nameNode := node.ChildByFieldName("name")
	if nameNode.IsNull() {
		return ""
	}

	return strings.TrimSpace(nameNode.Content(a.content))
}

func (a *phpAnalyzer) propertyHasRouterType(name string) bool {
	if len(a.propertyTypes) == 0 {
		return false
	}
	for _, occ := range a.propertyTypes[name] {
		if _, ok := canonicalRouterType(occ.Type); ok {
			return true
		}
	}
	return false
}

func (a *phpAnalyzer) collectPropertyTypes() map[string][]typeOccurrence {
	types := make(map[string][]typeOccurrence)
	if a.tree == nil {
		return types
	}
	root := a.tree.RootNode()
	if root.IsNull() {
		return types
	}

	uses := a.collectNamespaceUses(root)
	stack := []sitter.Node{root}

	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		switch node.Type() {
		case "property_declaration":
			for name, collected := range a.propertyTypesFromDeclaration(node, uses) {
				if len(collected) == 0 {
					continue
				}
				types[name] = mergeTypeOccurrences(types[name], collected)
			}
		case "property_promotion_parameter":
			if name, collected, ok := a.propertyTypeFromPromotion(node, uses); ok && len(collected) > 0 {
				types[name] = mergeTypeOccurrences(types[name], collected)
			}
		}

		for i := node.NamedChildCount(); i > 0; i-- {
			stack = append(stack, node.NamedChild(i-1))
		}
	}

	return types
}

func (a *phpAnalyzer) collectFunctionVariableTypes() map[string]map[string][]typeOccurrence {
	result := make(map[string]map[string][]typeOccurrence)
	if a.tree == nil {
		return result
	}
	root := a.tree.RootNode()
	if root.IsNull() {
		return result
	}

	uses := a.collectNamespaceUses(root)
	stack := []sitter.Node{root}

	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		switch node.Type() {
		case "method_declaration", "function_definition", "function_declaration":
			funcName := a.functionIdentifier(node)
			result[funcName] = a.collectVariableTypesForFunction(node, uses)
		}

		for i := uint32(0); i < node.NamedChildCount(); i++ {
			stack = append(stack, node.NamedChild(i))
		}
	}

	return result
}

func (a *phpAnalyzer) collectVariableTypesForFunction(node sitter.Node, uses map[string]string) map[string][]typeOccurrence {
	types := make(map[string][]typeOccurrence)
	params := node.ChildByFieldName("parameters")
	if !params.IsNull() {
		for i := uint32(0); i < params.NamedChildCount(); i++ {
			param := params.NamedChild(i)
			nameNode := param.ChildByFieldName("name")
			name := a.variableNameFromNode(nameNode)
			if name == "" {
				continue
			}
			typeNames := a.collectTypeNames(param.ChildByFieldName("type"), uses)
			if len(typeNames) == 0 {
				continue
			}
			line := int(param.StartPoint().Row) + 1
			occ := occurrencesFromTypeNames(typeNames, line)
			types[name] = mergeTypeOccurrences(types[name], occ)
		}
	}

	body := a.functionBodyNode(node)
	if body.IsNull() {
		return types
	}

	pendingDoc := make(map[string][]string)
	for i := uint32(0); i < body.NamedChildCount(); i++ {
		stmt := body.NamedChild(i)
		switch stmt.Type() {
		case "comment":
			varName, docTypes := a.parseDocblockVar(stmt, uses)
			if varName != "" && len(docTypes) > 0 {
				pendingDoc[varName] = docTypes
			}
			continue
		case "expression_statement":
			expr := stmt.NamedChild(0)
			if expr.IsNull() || expr.Type() != "assignment_expression" {
				pendingDoc = make(map[string][]string)
				continue
			}
			left := expr.ChildByFieldName("left")
			varName := a.variableNameFromNode(left)
			if varName == "" {
				pendingDoc = make(map[string][]string)
				continue
			}
			line := int(expr.StartPoint().Row) + 1
			right := expr.ChildByFieldName("right")
			inferred := a.inferExpressionTypeNames(right, uses, types, line-1)
			docs := pendingDoc[varName]
			combined := mergeTypeNameLists(docs, inferred)
			if len(combined) > 0 {
				occ := occurrencesFromTypeNames(combined, line)
				types[varName] = mergeTypeOccurrences(types[varName], occ)
			}
			delete(pendingDoc, varName)
		default:
			// reset pending docblock hints when encountering unrelated statements
			pendingDoc = make(map[string][]string)
			continue
		}
		// docblocks apply only to immediately following statement
		pendingDoc = make(map[string][]string)
	}

	return types
}

func (a *phpAnalyzer) functionBodyNode(node sitter.Node) sitter.Node {
	if body := node.ChildByFieldName("body"); !body.IsNull() {
		return body
	}
	return sitter.Node{}
}

func (a *phpAnalyzer) functionIdentifier(node sitter.Node) string {
	name := a.functionNameFromNode(node)
	if name != "" {
		return name
	}
	return fmt.Sprintf("anonymous@%d", int(node.StartPoint().Row)+1)
}

func (a *phpAnalyzer) functionNameFromNode(node sitter.Node) string {
	nameNode := node.ChildByFieldName("name")
	if nameNode.IsNull() {
		return ""
	}
	return strings.TrimSpace(nameNode.Content(a.content))
}

func (a *phpAnalyzer) parseDocblockVar(node sitter.Node, uses map[string]string) (string, []string) {
	text := node.Content(a.content)
	matches := docblockVarRe.FindStringSubmatch(text)
	if len(matches) < 3 {
		return "", nil
	}
	typeExpr := matches[1]
	varName := matches[2]
	parts := strings.Split(typeExpr, "|")
	types := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		nullHint := false
		if strings.HasPrefix(part, "?") {
			nullHint = true
			part = strings.TrimPrefix(part, "?")
		}
		if nullHint {
			types = mergeTypeNameLists(types, []string{"null"})
		}
		lower := strings.ToLower(part)
		if lower == "null" {
			types = mergeTypeNameLists(types, []string{"null"})
			continue
		}
		if strings.HasSuffix(part, "[]") {
			types = mergeTypeNameLists(types, []string{"array"})
			continue
		}
		resolved := a.resolveRawTypeName(part, uses)
		if resolved == "" {
			resolved = part
		}
		types = mergeTypeNameLists(types, []string{resolved})
	}
	return varName, types
}

func (a *phpAnalyzer) inferExpressionTypeNames(expr sitter.Node, uses map[string]string, current map[string][]typeOccurrence, line int) []string {
	if expr.IsNull() {
		return nil
	}
	switch expr.Type() {
	case "member_access_expression", "nullsafe_member_access_expression":
		if name := a.routerPropertyNameFromMemberAccess(expr); name != "" {
			return typeNamesFromOccurrences(a.propertyTypes[name])
		}
		object := expr.ChildByFieldName("object")
		if object.IsNull() {
			return nil
		}
		if object.Type() == "variable_name" {
			varName := a.variableNameFromNode(object)
			if varName != "" {
				return typeNamesAtOrBefore(current[varName], line)
			}
		}
	case "variable_name":
		varName := a.variableNameFromNode(expr)
		if varName == "" {
			return nil
		}
		return typeNamesAtOrBefore(current[varName], line)
	case "qualified_name", "relative_name", "name":
		candidate := strings.TrimSpace(expr.Content(a.content))
		if candidate == "" {
			return nil
		}
		resolved := a.resolveRawTypeName(candidate, uses)
		if resolved == "" {
			resolved = candidate
		}
		return []string{resolved}
	case "string":
		return []string{"string"}
	case "integer":
		return []string{"int"}
	case "float", "floating_point_number", "floating_literal":
		return []string{"float"}
	case "true", "false", "boolean":
		return []string{"bool"}
	case "null", "null_literal":
		return []string{"null"}
	case "array_creation_expression":
		return []string{"array"}
	case "object_creation_expression":
		return a.collectTypeNames(expr.ChildByFieldName("type"), uses)
	case "cast_expression":
		return a.collectTypeNames(expr.ChildByFieldName("type"), uses)
	case "parenthesized_expression":
		inner := expr.ChildByFieldName("expression")
		if inner.IsNull() && expr.NamedChildCount() > 0 {
			inner = expr.NamedChild(0)
		}
		return a.inferExpressionTypeNames(inner, uses, current, line)
	}
	return nil
}

func mergeTypeNameLists(existing, additions []string) []string {
	if len(additions) == 0 && len(existing) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(existing)+len(additions))
	result := make([]string, 0, len(existing)+len(additions))
	for _, name := range existing {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, name)
	}
	for _, name := range additions {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, name)
	}
	return result
}

func occurrencesFromTypeNames(names []string, line int) []typeOccurrence {
	if len(names) == 0 {
		return nil
	}
	occ := make([]typeOccurrence, 0, len(names))
	for _, typ := range names {
		typ = strings.TrimSpace(typ)
		if typ == "" {
			continue
		}
		occ = append(occ, typeOccurrence{Type: typ, Line: line})
	}
	return occ
}

func typeNamesFromOccurrences(entries []typeOccurrence) []string {
	if len(entries) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(entries))
	types := make([]string, 0, len(entries))
	for _, occ := range entries {
		key := strings.ToLower(occ.Type)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		types = append(types, occ.Type)
	}
	return types
}

func typeNamesAtOrBefore(entries []typeOccurrence, line int) []string {
	if len(entries) == 0 {
		return nil
	}
	maxLine := -1
	for _, occ := range entries {
		if line >= 0 && occ.Line > line {
			continue
		}
		if occ.Line > maxLine {
			maxLine = occ.Line
		}
	}
	if maxLine == -1 {
		return nil
	}
	seen := make(map[string]struct{})
	var result []string
	for _, occ := range entries {
		if occ.Line != maxLine {
			continue
		}
		key := strings.ToLower(occ.Type)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, occ.Type)
	}
	return result
}

func (a *phpAnalyzer) enclosingFunctionName(node sitter.Node) string {
	for cur := node; !cur.IsNull(); cur = cur.Parent() {
		switch cur.Type() {
		case "method_declaration", "function_definition", "function_declaration":
			return a.functionIdentifier(cur)
		}
	}
	return ""
}

func (a *phpAnalyzer) variableHasRouterType(funcName, varName string, line int) bool {
	if funcName == "" || varName == "" {
		return false
	}
	if a.functionVars == nil {
		return false
	}
	vars, ok := a.functionVars[funcName]
	if !ok {
		return false
	}
	entries, ok := vars[varName]
	if !ok {
		return false
	}
	types := typeNamesAtOrBefore(entries, line)
	for _, typ := range types {
		if _, ok := canonicalRouterType(typ); ok {
			return true
		}
	}
	return false
}

func (a *phpAnalyzer) collectNamespaceUses(root sitter.Node) map[string]string {
	uses := make(map[string]string)
	if root.IsNull() {
		return uses
	}

	stack := []sitter.Node{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if node.Type() == "namespace_use_declaration" {
			if typeNode := node.ChildByFieldName("type"); !typeNode.IsNull() {
				continue
			}
			prefix := ""
			for i := uint32(0); i < node.NamedChildCount(); i++ {
				child := node.NamedChild(i)
				switch child.Type() {
				case "namespace_name":
					prefix = normalizeFQN(child.Content(a.content))
				case "namespace_use_group":
					for j := uint32(0); j < child.NamedChildCount(); j++ {
						if child.NamedChild(j).Type() == "namespace_use_clause" {
							a.addUseClause(child.NamedChild(j), prefix, uses)
						}
					}
				case "namespace_use_clause":
					a.addUseClause(child, "", uses)
				}
			}
			continue
		}

		for i := uint32(0); i < node.NamedChildCount(); i++ {
			stack = append(stack, node.NamedChild(i))
		}
	}

	return uses
}

func (a *phpAnalyzer) addUseClause(clause sitter.Node, prefix string, uses map[string]string) {
	if clause.IsNull() {
		return
	}

	aliasNode := clause.ChildByFieldName("alias")
	alias := ""
	if !aliasNode.IsNull() {
		alias = strings.TrimSpace(aliasNode.Content(a.content))
	}

	var nameNode sitter.Node
	for i := uint32(0); i < clause.NamedChildCount(); i++ {
		if clause.FieldNameForNamedChild(i) == "alias" {
			continue
		}
		child := clause.NamedChild(i)
		switch child.Type() {
		case "qualified_name", "relative_name", "name":
			nameNode = child
		}
		if !nameNode.IsNull() {
			break
		}
	}

	if nameNode.IsNull() {
		return
	}

	base := strings.TrimSpace(nameNode.Content(a.content))
	full := base
	if prefix != "" {
		full = prefix + "\\" + strings.TrimLeft(base, "\\")
	}
	full = normalizeFQN(full)
	if full == "" {
		return
	}

	if alias == "" {
		alias = shortName(full)
	}

	lowerAlias := strings.ToLower(alias)
	if lowerAlias != "" {
		uses[lowerAlias] = full
	}
	uses[strings.ToLower(full)] = full
}

func (a *phpAnalyzer) propertyTypesFromDeclaration(node sitter.Node, uses map[string]string) map[string][]typeOccurrence {
	result := make(map[string][]typeOccurrence)

	typeNode := node.ChildByFieldName("type")
	if typeNode.IsNull() {
		return result
	}

	typeNames := a.collectTypeNames(typeNode, uses)
	if len(typeNames) == 0 {
		return result
	}

	for i := uint32(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Type() != "property_element" {
			continue
		}
		line := int(child.StartPoint().Row) + 1
		nameNode := child.ChildByFieldName("name")
		name := a.variableNameFromNode(nameNode)
		if name == "" {
			continue
		}
		occ := make([]typeOccurrence, 0, len(typeNames))
		for _, typ := range typeNames {
			occ = append(occ, typeOccurrence{Type: typ, Line: line})
		}
		result[name] = append(result[name], occ...)
	}

	return result
}

func (a *phpAnalyzer) propertyTypeFromPromotion(node sitter.Node, uses map[string]string) (string, []typeOccurrence, bool) {
	typeNode := node.ChildByFieldName("type")
	if typeNode.IsNull() {
		return "", nil, false
	}

	typeNames := a.collectTypeNames(typeNode, uses)
	if len(typeNames) == 0 {
		return "", nil, false
	}

	nameNode := node.ChildByFieldName("name")
	name := a.variableNameFromNode(nameNode)
	if name == "" {
		return "", nil, false
	}

	line := int(node.StartPoint().Row) + 1
	occ := make([]typeOccurrence, 0, len(typeNames))
	for _, typ := range typeNames {
		occ = append(occ, typeOccurrence{Type: typ, Line: line})
	}

	return name, occ, true
}

func (a *phpAnalyzer) collectTypeNames(typeNode sitter.Node, uses map[string]string) []string {
	if typeNode.IsNull() {
		return nil
	}

	names := make([]string, 0)
	seen := make(map[string]struct{})
	var collect func(n sitter.Node)
	collect = func(n sitter.Node) {
		if n.IsNull() {
			return
		}
		switch n.Type() {
		case "named_type":
			if resolved := a.resolveNamedType(n, uses); resolved != "" {
				key := strings.ToLower(resolved)
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
					names = append(names, resolved)
				}
			}
		case "primitive_type":
			raw := strings.TrimSpace(n.Content(a.content))
			if raw != "" {
				raw = strings.ToLower(raw)
				if _, ok := seen[raw]; !ok {
					seen[raw] = struct{}{}
					names = append(names, raw)
				}
			}
		case "nullable_type":
			collect(n.ChildByFieldName("type"))
			if _, ok := seen["null"]; !ok {
				seen["null"] = struct{}{}
				names = append(names, "null")
			}
		case "union_type", "intersection_type":
			for i := uint32(0); i < n.NamedChildCount(); i++ {
				collect(n.NamedChild(i))
			}
		case "qualified_name", "relative_name", "name":
			candidate := strings.TrimSpace(n.Content(a.content))
			if candidate == "" {
				break
			}
			resolved := a.resolveRawTypeName(candidate, uses)
			if resolved == "" {
				break
			}
			key := strings.ToLower(resolved)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				names = append(names, resolved)
			}
		default:
			for i := uint32(0); i < n.NamedChildCount(); i++ {
				collect(n.NamedChild(i))
			}
			return
		}
	}
	collect(typeNode)

	return names
}

func (a *phpAnalyzer) resolveNamedType(node sitter.Node, uses map[string]string) string {
	var typeNode sitter.Node
	for i := uint32(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		switch child.Type() {
		case "qualified_name", "relative_name", "name":
			typeNode = child
		}
		if !typeNode.IsNull() {
			break
		}
	}

	raw := ""
	if !typeNode.IsNull() {
		raw = typeNode.Content(a.content)
	} else {
		raw = node.Content(a.content)
	}

	raw = normalizeFQN(raw)
	if raw == "" {
		return ""
	}

	lowered := strings.ToLower(raw)
	if full, ok := uses[lowered]; ok {
		return full
	}

	shortLower := strings.ToLower(shortName(raw))
	if full, ok := uses[shortLower]; ok {
		return full
	}

	return raw
}

func (a *phpAnalyzer) resolveRawTypeName(raw string, uses map[string]string) string {
	raw = normalizeFQN(raw)
	if raw == "" {
		return ""
	}

	lowered := strings.ToLower(raw)
	if full, ok := uses[lowered]; ok {
		return full
	}
	if full, ok := uses[strings.ToLower(shortName(raw))]; ok {
		return full
	}

	return raw
}

func mergeTypeOccurrences(existing, additions []typeOccurrence) []typeOccurrence {
	if len(additions) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing))
	for _, occ := range existing {
		key := strings.ToLower(occ.Type) + "#" + strconv.Itoa(occ.Line)
		seen[key] = struct{}{}
	}
	for _, add := range additions {
		if add.Type == "" {
			continue
		}
		key := strings.ToLower(add.Type) + "#" + strconv.Itoa(add.Line)
		if _, ok := seen[key]; ok {
			continue
		}
		existing = append(existing, add)
		seen[key] = struct{}{}
	}
	return existing
}

func canonicalRouterType(name string) (string, bool) {
	normalized := normalizeFQN(name)
	if normalized == "" {
		return "", false
	}
	if canonical, ok := routerCanonical[strings.ToLower(normalized)]; ok {
		return canonical, true
	}
	if canonical, ok := routerCanonical[strings.ToLower(shortName(normalized))]; ok {
		return canonical, true
	}
	return "", false
}

func normalizeFQN(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\\\", "\\"))
	name = strings.TrimLeft(name, "?\\")
	return name
}

func (a *phpAnalyzer) variableNameFromNode(node sitter.Node) string {
	if node.IsNull() {
		return ""
	}

	switch node.Type() {
	case "variable_name":
		for i := uint32(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child.Type() == "name" {
				return child.Content(a.content)
			}
		}
		content := node.Content(a.content)
		return strings.TrimPrefix(content, "$")
	case "by_ref":
		for i := uint32(0); i < node.NamedChildCount(); i++ {
			child := node.NamedChild(i)
			if child.Type() == "variable_name" {
				return a.variableNameFromNode(child)
			}
		}
	case "name":
		return node.Content(a.content)
	}

	content := strings.TrimSpace(node.Content(a.content))
	return strings.TrimPrefix(content, "$")
}
