package analyzer

import (
	"context"
	"regexp"
	"sort"
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
	propertyTypes  map[string][]string
}

const (
	urlGeneratorInterfaceFQN = "Symfony\\Component\\Routing\\Generator\\UrlGeneratorInterface"
	urlGeneratorFQN          = "Symfony\\Component\\Routing\\Generator\\UrlGenerator"
	routerInterfaceFQN       = "Symfony\\Component\\Routing\\RouterInterface"
	routerFQN                = "Symfony\\Component\\Routing\\Router"
)

var (
	routerTargetTypes = map[string]string{
		strings.ToLower(urlGeneratorInterfaceFQN): urlGeneratorInterfaceFQN,
		strings.ToLower(urlGeneratorFQN):          urlGeneratorFQN,
		strings.ToLower(routerInterfaceFQN):       routerInterfaceFQN,
		strings.ToLower(routerFQN):                routerFQN,
	}
	routerTargetShortTypes = map[string]string{
		strings.ToLower(shortName(urlGeneratorInterfaceFQN)): urlGeneratorInterfaceFQN,
		strings.ToLower(shortName(urlGeneratorFQN)):          urlGeneratorFQN,
		strings.ToLower(shortName(routerInterfaceFQN)):       routerInterfaceFQN,
		strings.ToLower(shortName(routerFQN)):                routerFQN,
	}
)

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

type phpRouteCallCtx struct {
	callNode sitter.Node
	argsNode sitter.Node
	argIndex int
	strNode  sitter.Node
	property string
}

func (a *phpAnalyzer) phpRouteNameCompletionItems(pos protocol.Position) []protocol.CompletionItem {
	found, prefix := a.isTypingPhpRouteName(pos)
	if !found {
		return nil
	}

	items := make([]protocol.CompletionItem, 0)
	kind := protocol.CompletionItemKindConstant

	for name, route := range a.routes {
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		detail := "Symfony route"
		var doc strings.Builder
		doc.WriteString("**Route:** `")
		doc.WriteString(name)
		doc.WriteString("`\n\n")
		if len(route.Parameters) > 0 {
			doc.WriteString("**Parameters:**\n")
			for _, param := range route.Parameters {
				doc.WriteString("- `")
				doc.WriteString(param)
				doc.WriteString("`\n")
			}
		} else {
			doc.WriteString("*No parameters*")
		}

		documentation := protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: doc.String(),
		}

		items = append(items, protocol.CompletionItem{
			Label:         name,
			Kind:          &kind,
			Detail:        &detail,
			Documentation: documentation,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		li, lj := items[i].Label, items[j].Label
		if len(li) != len(lj) {
			return len(li) < len(lj)
		}
		return li < lj
	})

	return items
}

func (a *phpAnalyzer) phpRouteParameterCompletionItems(pos protocol.Position) []protocol.CompletionItem {
	found, routeName, prefix := a.isTypingPhpRouteParameter(pos)
	if !found {
		return nil
	}

	route, ok := a.routes[routeName]
	if !ok {
		return nil
	}

	items := make([]protocol.CompletionItem, 0)
	kind := protocol.CompletionItemKindProperty

	for _, param := range route.Parameters {
		if !strings.HasPrefix(param, prefix) {
			continue
		}
		detail := "parameter for route " + routeName
		items = append(items, protocol.CompletionItem{
			Label:  param,
			Kind:   &kind,
			Detail: &detail,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		li, lj := items[i].Label, items[j].Label
		if len(li) != len(lj) {
			return len(li) < len(lj)
		}
		return li < lj
	})

	return items
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

		propertyName := a.routerPropertyNameFromMemberAccess(objectNode)
		if propertyName == "" || !a.propertyHasRouterType(propertyName) {
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

	switch value.Type() {
	case "string":
		return a.stringContent(value)
	case "string_content":
		parent := value.Parent()
		if !parent.IsNull() && parent.Type() == "string" {
			return a.stringContent(parent)
		}
	}

	return ""
}

func (a *phpAnalyzer) stringPrefix(str sitter.Node, pos protocol.Position) string {
	if str.IsNull() {
		return ""
	}
	if str.Type() == "string_content" {
		str = str.Parent()
	}
	if str.IsNull() || str.Type() != "string" {
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
	if str.IsNull() {
		return ""
	}
	if str.Type() == "string_content" {
		str = str.Parent()
	}
	if str.IsNull() || str.Type() != "string" {
		return ""
	}
	sb, eb := int(str.StartByte()), int(str.EndByte())
	if eb-sb < 2 {
		return ""
	}
	return string(a.content[sb+1 : eb-1])
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

		for i := uint32(0); i < namedCount; i++ {
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
	for _, typ := range a.propertyTypes[name] {
		if _, ok := canonicalRouterType(typ); ok {
			return true
		}
	}
	return false
}

func (a *phpAnalyzer) collectPropertyTypes() map[string][]string {
	types := make(map[string][]string)
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
				types[name] = mergeTypeLists(types[name], collected)
			}
		case "property_promotion_parameter":
			if name, collected, ok := a.propertyTypeFromPromotion(node, uses); ok && len(collected) > 0 {
				types[name] = mergeTypeLists(types[name], collected)
			}
		}

		for i := node.NamedChildCount(); i > 0; i-- {
			stack = append(stack, node.NamedChild(i-1))
		}
	}

	return types
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
			a.processNamespaceUseDeclaration(node, uses)
			continue
		}

		for i := uint32(0); i < node.NamedChildCount(); i++ {
			stack = append(stack, node.NamedChild(i))
		}
	}

	return uses
}

func (a *phpAnalyzer) processNamespaceUseDeclaration(node sitter.Node, uses map[string]string) {
	if node.IsNull() {
		return
	}

	if typeNode := node.ChildByFieldName("type"); !typeNode.IsNull() {
		// Ignore use function/const declarations
		return
	}

	prefix := ""
	for i := uint32(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		switch child.Type() {
		case "namespace_name":
			prefix = normalizeFQN(child.Content(a.content))
		case "namespace_use_group":
			a.processNamespaceUseGroup(child, prefix, uses)
		case "namespace_use_clause":
			a.addUseClause(child, "", uses)
		}
	}
}

func (a *phpAnalyzer) processNamespaceUseGroup(node sitter.Node, prefix string, uses map[string]string) {
	if node.IsNull() {
		return
	}

	for i := uint32(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Type() == "namespace_use_clause" {
			a.addUseClause(child, prefix, uses)
		}
	}
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
			break
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

func (a *phpAnalyzer) propertyTypesFromDeclaration(node sitter.Node, uses map[string]string) map[string][]string {
	result := make(map[string][]string)

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
		nameNode := child.ChildByFieldName("name")
		name := a.variableNameFromNode(nameNode)
		if name == "" {
			continue
		}
		result[name] = append(result[name], typeNames...)
	}

	return result
}

func (a *phpAnalyzer) propertyTypeFromPromotion(node sitter.Node, uses map[string]string) (string, []string, bool) {
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

	return name, typeNames, true
}

func (a *phpAnalyzer) collectTypeNames(typeNode sitter.Node, uses map[string]string) []string {
	if typeNode.IsNull() {
		return nil
	}

	names := make([]string, 0)
	seen := make(map[string]struct{})
	stack := []sitter.Node{typeNode}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if node.Type() == "named_type" {
			if resolved := a.resolveNamedType(node, uses); resolved != "" {
				key := strings.ToLower(resolved)
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
					names = append(names, resolved)
				}
			}
		}

		for i := uint32(0); i < node.NamedChildCount(); i++ {
			stack = append(stack, node.NamedChild(i))
		}
	}

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

func mergeTypeLists(existing, additions []string) []string {
	if len(additions) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		seen[strings.ToLower(e)] = struct{}{}
	}
	for _, add := range additions {
		key := strings.ToLower(add)
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

	if canonical, ok := routerTargetTypes[strings.ToLower(normalized)]; ok {
		return canonical, true
	}

	if canonical, ok := routerTargetShortTypes[strings.ToLower(shortName(normalized))]; ok {
		return canonical, true
	}

	return "", false
}

func normalizeFQN(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	for strings.HasPrefix(name, "?") {
		name = strings.TrimPrefix(name, "?")
	}
	for strings.HasPrefix(name, "\\") {
		name = strings.TrimPrefix(name, "\\")
	}
	name = strings.ReplaceAll(name, "\\\\", "\\")
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
