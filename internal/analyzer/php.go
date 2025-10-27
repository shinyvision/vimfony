package analyzer

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	phpforest "github.com/alexaandru/go-sitter-forest/php"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	php "github.com/shinyvision/vimfony/internal/php"
	"github.com/shinyvision/vimfony/internal/twig"
	"github.com/shinyvision/vimfony/internal/utils"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type phpAnalyzer struct {
	mu             sync.RWMutex
	attributeQuery *sitter.Query
	servicesRe     *regexp.Regexp
	container      *config.ContainerConfig
	routes         config.RoutesMap
	doc            *php.Document
	docStore       *php.DocumentStore
	psr4           config.Psr4Map
	path           string
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
	abstractControllerFQN    = "Symfony\\Bundle\\FrameworkBundle\\Controller\\AbstractController"
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

func NewPHPAnalyzer() Analyzer {
	lang := sitter.NewLanguage(phpforest.GetLanguage())
	attributeQuery, _ := sitter.NewQuery(lang, []byte(`
      (attribute
        [(qualified_name) (name)] @name
      ) @attr
    `))
	servicesRe := regexp.MustCompile(`['"\\](@?[A-Za-z0-9_.\\-]*)$`)
	return &phpAnalyzer{
		attributeQuery: attributeQuery,
		servicesRe:     servicesRe,
		doc:            php.NewDocument(),
	}
}

func (a *phpAnalyzer) Changed(code []byte, change *sitter.InputEdit) error {
	if a.doc == nil {
		a.doc = php.NewDocument()
	}
	if err := a.doc.Update(code, change); err != nil {
		return err
	}
	a.mu.Lock()
	path := a.path
	doc := a.doc
	store := a.docStore
	a.mu.Unlock()
	if store != nil && doc != nil && path != "" {
		store.RegisterOpen(path, doc)
	}
	return nil
}

func (a *phpAnalyzer) Close() {
	a.mu.Lock()
	path := a.path
	store := a.docStore
	a.mu.Unlock()
	if store != nil && path != "" {
		store.Close(path)
	}
}

func (a *phpAnalyzer) indexSnapshot() php.IndexedTree {
	if a.doc == nil {
		return php.IndexedTree{}
	}
	var snapshot php.IndexedTree
	a.doc.Read(func(_ *sitter.Tree, _ []byte, index php.IndexedTree) {
		snapshot = index
	})
	return snapshot
}

func (a *phpAnalyzer) isInAutoconfigure(pos protocol.Position) (bool, string) {
	if a.attributeQuery == nil {
		return false, ""
	}

	var (
		found  bool
		prefix string
	)

	if a.doc == nil {
		return false, ""
	}

	a.doc.Read(func(tree *sitter.Tree, content []byte, _ php.IndexedTree) {
		if tree == nil || found {
			return
		}

		point, ok := lspPosToPoint(pos, content)
		if !ok {
			return
		}

		root := tree.RootNode()
		q := a.attributeQuery
		qc := sitter.NewQueryCursor()
		it := qc.Matches(q, root, content)

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
			if shortName(nameNode.Content(content)) != "Autoconfigure" {
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

			lineUntilCaret := linePrefixAtPoint(content, point)
			if m := a.servicesRe.FindSubmatch(lineUntilCaret); len(m) > 1 {
				found = true
				prefix = string(m[1])
				return
			}
			found = true
			prefix = ""
			return
		}
	})

	return found, prefix
}

func (a *phpAnalyzer) SetContainerConfig(container *config.ContainerConfig) {
	a.mu.Lock()
	a.container = container
	doc := a.doc
	a.mu.Unlock()
	if doc != nil {
		root := ""
		if container != nil {
			root = container.WorkspaceRoot
		}
		doc.SetWorkspaceRoot(root)
	}
}

func (a *phpAnalyzer) SetDocumentPath(path string) {
	clean := path
	if clean != "" {
		clean = filepath.Clean(clean)
	}
	a.mu.Lock()
	a.path = clean
	doc := a.doc
	store := a.docStore
	a.mu.Unlock()
	if doc != nil && clean != "" {
		doc.SetURI(utils.PathToURI(clean))
	}
	if store != nil && doc != nil && clean != "" {
		store.RegisterOpen(clean, doc)
	}
}

func (a *phpAnalyzer) SetRoutes(routes *config.RoutesMap) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if routes == nil {
		a.routes = nil
		return
	}
	a.routes = *routes
}

func (a *phpAnalyzer) SetPsr4Map(psr4 *config.Psr4Map) {
	a.mu.Lock()
	if psr4 == nil {
		a.psr4 = nil
		doc := a.doc
		a.mu.Unlock()
		if doc != nil {
			doc.SetPsr4Map(nil)
		}
		return
	}
	a.psr4 = *psr4
	doc := a.doc
	a.mu.Unlock()
	if doc != nil {
		doc.SetPsr4Map(a.psr4)
	}
}

func (a *phpAnalyzer) SetDocumentStore(store *php.DocumentStore) {
	a.mu.Lock()
	a.docStore = store
	path := a.path
	doc := a.doc
	a.mu.Unlock()
	if store != nil && doc != nil && path != "" {
		store.RegisterOpen(path, doc)
	}
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

func (a *phpAnalyzer) OnDefinition(pos protocol.Position) ([]protocol.Location, error) {
	var content string
	if a.doc != nil {
		a.doc.Read(func(_ *sitter.Tree, data []byte, _ php.IndexedTree) {
			content = string(data)
		})
	}

	a.mu.RLock()
	container := a.container
	psr4 := a.psr4
	a.mu.RUnlock()

	if container == nil {
		return nil, nil
	}

	if locs, ok := a.resolveRouteDefinition(pos); ok {
		return locs, nil
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

	if className, ok := php.PathAt(content, pos); ok {
		if locs, ok := resolveClassLocations(className, container, psr4); ok {
			return locs, nil
		}
	}

	if locs, ok := a.resolveServiceDefinition(content, pos, container, psr4); ok {
		return locs, nil
	}

	return nil, nil
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
	if a.doc == nil {
		return phpRouteCallCtx{}, false
	}

	node, content, index, ok := a.doc.GetNodeAt(pos)
	if !ok {
		return phpRouteCallCtx{}, false
	}

	controllerTarget := strings.ToLower(normalizeFQN(abstractControllerFQN))
	if controllerTarget == "" {
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
		if nameNode.IsNull() {
			return phpRouteCallCtx{}, false
		}

		objectNode := callNode.ChildByFieldName("object")
		if objectNode.IsNull() {
			return phpRouteCallCtx{}, false
		}

		methodName := strings.TrimSpace(nameNode.Content(content))
		switch methodName {
		case "generate":
			callLine := int(callNode.StartPoint().Row) + 1
			funcName := ""
			for candidate := callNode; !candidate.IsNull(); candidate = candidate.Parent() {
				switch candidate.Type() {
				case "method_declaration", "function_definition", "function_declaration":
					funcName = functionIdentifierContent(content, candidate)
					break
				}
				if funcName != "" {
					break
				}
			}

			propertyName := routerPropertyNameFromMemberAccessContent(content, objectNode)
			if propertyName != "" {
				if !propertyHasRouterTypeIndex(index, propertyName) {
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
				varName := php.VariableNameFromNode(objectNode, content)
				if varName == "" {
					return phpRouteCallCtx{}, false
				}
				if !variableHasRouterTypeIndex(index, funcName, varName, callLine) {
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
		case "generateUrl", "redirectToRoute":
			if !isThisVariable(objectNode, content) {
				return phpRouteCallCtx{}, false
			}
			if !classExtendsAbstractControllerIndex(index, callNode, controllerTarget) {
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
			}, true
		default:
			return phpRouteCallCtx{}, false
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
	var result string
	if a.doc == nil {
		return ""
	}
	a.doc.Read(func(_ *sitter.Tree, content []byte, _ php.IndexedTree) {
		sb, eb := int(str.StartByte()), int(str.EndByte())
		if eb-sb < 2 {
			result = ""
			return
		}
		inner := content[sb+1 : eb-1]
		caret := lspPosToByteOffset(content, pos)
		if caret > sb && caret < eb {
			rel := caret - sb - 1
			if rel >= 0 && rel <= len(inner) {
				result = string(inner[:rel])
				return
			}
		}
		result = string(inner)
	})
	return result
}

func (a *phpAnalyzer) stringContent(str sitter.Node) string {
	s, e, ok := a.stringInnerBounds(str)
	if !ok {
		return ""
	}
	var result string
	if a.doc == nil {
		return ""
	}
	a.doc.Read(func(_ *sitter.Tree, content []byte, _ php.IndexedTree) {
		if s >= 0 && e <= len(content) && s < e {
			result = string(content[s:e])
		}
	})
	return result
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

	var result string
	if a.doc == nil {
		return ""
	}
	a.doc.Read(func(_ *sitter.Tree, content []byte, _ php.IndexedTree) {
		result = routerPropertyNameFromMemberAccessContent(content, node)
	})
	return result
}

func isThisVariable(node sitter.Node, content []byte) bool {
	if node.IsNull() || node.Type() != "variable_name" {
		return false
	}
	return strings.TrimSpace(node.Content(content)) == "$this"
}

func (a *phpAnalyzer) propertyHasRouterType(name string) bool {
	result := false
	if a.doc == nil {
		return false
	}
	a.doc.Read(func(_ *sitter.Tree, _ []byte, index php.IndexedTree) {
		result = propertyHasRouterTypeIndex(index, name)
	})
	return result
}

func (a *phpAnalyzer) classExtendsAbstractController(node sitter.Node) bool {
	target := strings.ToLower(normalizeFQN(abstractControllerFQN))
	if target == "" {
		return false
	}
	result := false
	if a.doc == nil {
		return false
	}
	a.doc.Read(func(_ *sitter.Tree, _ []byte, index php.IndexedTree) {
		result = classExtendsAbstractControllerIndex(index, node, target)
	})
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

func (a *phpAnalyzer) functionIdentifier(node sitter.Node) string {
	nameNode := node.ChildByFieldName("name")
	if !nameNode.IsNull() {
		var result string
		if a.doc == nil {
			return ""
		}
		a.doc.Read(func(_ *sitter.Tree, content []byte, _ php.IndexedTree) {
			result = functionIdentifierContent(content, node)
		})
		return result
	}
	return fmt.Sprintf("anonymous@%d", int(node.StartPoint().Row)+1)
}

func (a *phpAnalyzer) variableHasRouterType(funcName, varName string, line int) bool {
	if funcName == "" || varName == "" {
		return false
	}
	result := false
	if a.doc == nil {
		return false
	}
	a.doc.Read(func(_ *sitter.Tree, _ []byte, index php.IndexedTree) {
		result = variableHasRouterTypeIndex(index, funcName, varName, line)
	})
	return result
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

func routerPropertyNameFromMemberAccessContent(content []byte, node sitter.Node) string {
	objectNode := node.ChildByFieldName("object")
	if objectNode.IsNull() {
		return ""
	}

	switch objectNode.Type() {
	case "variable_name":
		if strings.TrimSpace(objectNode.Content(content)) != "$this" {
			return ""
		}
	default:
		return ""
	}

	nameNode := node.ChildByFieldName("name")
	if nameNode.IsNull() {
		return ""
	}

	return strings.TrimSpace(nameNode.Content(content))
}

func propertyHasRouterTypeIndex(index php.IndexedTree, name string) bool {
	if len(index.Properties) == 0 {
		return false
	}
	entries, ok := index.Properties[name]
	if !ok {
		return false
	}
	for _, occ := range entries {
		if _, ok := canonicalRouterType(occ.Type); ok {
			return true
		}
	}
	return false
}

func classExtendsAbstractControllerIndex(index php.IndexedTree, node sitter.Node, target string) bool {
	for cur := node; !cur.IsNull(); cur = cur.Parent() {
		if cur.Type() != "class_declaration" {
			continue
		}
		info, ok := index.Classes[uint32(cur.StartByte())]
		if !ok {
			return false
		}
		for _, ext := range info.Extends {
			if strings.ToLower(normalizeFQN(ext)) == target {
				return true
			}
		}
		return false
	}
	return false
}

func functionIdentifierContent(content []byte, node sitter.Node) string {
	nameNode := node.ChildByFieldName("name")
	if !nameNode.IsNull() {
		return strings.TrimSpace(nameNode.Content(content))
	}
	return fmt.Sprintf("anonymous@%d", int(node.StartPoint().Row)+1)
}

func variableHasRouterTypeIndex(index php.IndexedTree, funcName, varName string, line int) bool {
	scope, ok := index.Variables[funcName]
	if !ok || scope.Variables == nil {
		return false
	}
	entries, ok := scope.Variables[varName]
	if !ok {
		return false
	}
	types := php.TypeNamesAtOrBefore(entries, line)
	for _, typ := range types {
		if _, ok := canonicalRouterType(typ); ok {
			return true
		}
	}
	return false
}

func (a *phpAnalyzer) variableNameFromNode(node sitter.Node) string {
	var result string
	if a.doc == nil {
		return ""
	}
	a.doc.Read(func(_ *sitter.Tree, content []byte, _ php.IndexedTree) {
		result = php.VariableNameFromNode(node, content)
	})
	return result
}

func (a *phpAnalyzer) resolveServiceDefinition(content string, pos protocol.Position, container *config.ContainerConfig, psr4 config.Psr4Map) ([]protocol.Location, bool) {
	if container == nil || len(container.ServiceClasses) == 0 {
		return nil, false
	}
	line, ok := lineAt(content, int(pos.Line))
	if !ok || line == "" {
		return nil, false
	}
	offset := int(pos.Character)
	if offset < 0 {
		offset = 0
	}
	if offset > len(line) {
		offset = len(line)
	}

	isServiceChar := func(b byte) bool {
		return (b >= 'a' && b <= 'z') ||
			(b >= 'A' && b <= 'Z') ||
			(b >= '0' && b <= '9') ||
			b == '_' || b == '.' || b == '-' || b == '\\'
	}

	left, right := 0, 0
	for {
		if offset-left == 0 || !isServiceChar(line[offset-left-1]) {
			break
		}
		left++
	}
	for {
		if offset+right == len(line) || !isServiceChar(line[offset+right]) {
			break
		}
		right++
	}
	if left == 0 && right == 0 {
		return nil, false
	}
	serviceID := line[offset-left : offset+right]
	if serviceID == "" {
		return nil, false
	}

	return resolveServiceIDLocations(serviceID, container, psr4)
}

func (a *phpAnalyzer) resolveRouteDefinition(pos protocol.Position) ([]protocol.Location, bool) {
	a.mu.RLock()
	container := a.container
	psr4 := a.psr4
	routes := a.routes
	store := a.docStore
	if container == nil || len(psr4) == 0 || len(routes) == 0 || store == nil {
		a.mu.RUnlock()
		return nil, false
	}
	ctx, ok := a.phpRouteContextAt(pos)
	if !ok || ctx.argIndex != 0 {
		a.mu.RUnlock()
		return nil, false
	}
	routeName := a.stringContent(ctx.strNode)
	if routeName == "" {
		routeName = a.phpRouteNameFromArgs(ctx.argsNode)
	}
	if routeName == "" {
		a.mu.RUnlock()
		return nil, false
	}
	route, ok := routes[routeName]
	a.mu.RUnlock()
	if !ok || route.Controller == "" {
		return nil, false
	}

	doc, uri, ok := routeDocument(route, container, psr4, store)
	if !ok {
		return nil, false
	}

	locs := resolveRouteLocations(route, uri, doc)
	if len(locs) == 0 {
		return nil, false
	}

	return locs, true
}
