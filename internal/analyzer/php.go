package analyzer

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	phpforest "github.com/alexaandru/go-sitter-forest/php"
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	php "github.com/shinyvision/vimfony/internal/php"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type phpAnalyzer struct {
	parser              *sitter.Parser
	mu                  sync.RWMutex
	attributeQuery      *sitter.Query
	servicesRe          *regexp.Regexp
	tree                *sitter.Tree
	content             []byte
	container           *config.ContainerConfig
	routes              config.RoutesMap
	index               php.IndexedTree
	staticAnalyzer      *php.StaticAnalyzer
	analysisTimer       *time.Timer
	analysisVersion     int64
	lastAnalyzedVersion int64
	dirtyRanges         []php.ByteRange
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

const analysisDebounceInterval = 500 * time.Millisecond

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
	p := sitter.NewParser()
	lang := sitter.NewLanguage(phpforest.GetLanguage())
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
		staticAnalyzer: php.NewStaticAnalyzer(),
	}
}

func (a *phpAnalyzer) Changed(code []byte, change *sitter.InputEdit) error {
	a.mu.Lock()

	a.content = code
	if a.tree != nil && change != nil {
		a.tree.Edit(*change)
	}
	newTree, err := a.parser.ParseString(context.Background(), a.tree, code)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	if a.tree != nil {
		a.tree.Close()
	}
	a.tree = newTree

	version := a.analysisVersion + 1
	a.analysisVersion = version

	if change == nil {
		a.dirtyRanges = nil
		if a.analysisTimer != nil {
			a.analysisTimer.Stop()
			a.analysisTimer = nil
		}
	} else {
		a.recordDirtyRangeLocked(change)
	}

	immediate := change == nil

	if !immediate {
		if a.analysisTimer != nil {
			a.analysisTimer.Stop()
		}
		timer := time.AfterFunc(analysisDebounceInterval, func() {
			a.runAnalysis(version)
		})
		a.analysisTimer = timer
	}

	a.mu.Unlock()

	if immediate {
		a.runAnalysis(version)
	}
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

func (a *phpAnalyzer) recordDirtyRangeLocked(edit *sitter.InputEdit) {
	if edit == nil {
		a.dirtyRanges = nil
		return
	}
	rangeStart := uint32(edit.StartIndex)
	rangeEnd := uint32(edit.NewEndIndex)
	if edit.OldEndIndex > edit.NewEndIndex {
		rangeEnd = uint32(edit.OldEndIndex)
	}
	if rangeStart > rangeEnd {
		rangeStart, rangeEnd = rangeEnd, rangeStart
	}
	if rangeStart == rangeEnd {
		rangeEnd++
	}
	merged := appendRange(a.dirtyRanges, php.ByteRange{Start: rangeStart, End: rangeEnd})
	a.dirtyRanges = merged
}

func appendRange(ranges []php.ByteRange, rng php.ByteRange) []php.ByteRange {
	ranges = append(ranges, rng)
	if len(ranges) == 0 {
		return ranges
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].Start == ranges[j].Start {
			return ranges[i].End < ranges[j].End
		}
		return ranges[i].Start < ranges[j].Start
	})

	merged := make([]php.ByteRange, 0, len(ranges))
	current := ranges[0]
	for _, r := range ranges[1:] {
		if r.Start <= current.End {
			if r.End > current.End {
				current.End = r.End
			}
			continue
		}
		merged = append(merged, current)
		current = r
	}
	merged = append(merged, current)
	return merged
}

func (a *phpAnalyzer) runAnalysis(version int64) {
	a.mu.RLock()
	if a.analysisVersion != version || a.tree == nil {
		a.mu.RUnlock()
		return
	}
	treeCopy := a.tree.Copy()
	dirty := append([]php.ByteRange(nil), a.dirtyRanges...)
	contentCopy := append([]byte(nil), a.content...)
	analyzer := a.staticAnalyzer
	a.mu.RUnlock()

	if analyzer == nil || treeCopy == nil {
		if treeCopy != nil {
			treeCopy.Close()
		}
		return
	}
	defer treeCopy.Close()

	index := analyzer.Update(&contentCopy, treeCopy, dirty)

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.analysisVersion != version {
		return
	}
	a.index = index
	a.lastAnalyzedVersion = version
	a.dirtyRanges = nil
	if a.analysisTimer != nil {
		a.analysisTimer.Stop()
		a.analysisTimer = nil
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
		if nameNode.IsNull() {
			return phpRouteCallCtx{}, false
		}

		objectNode := callNode.ChildByFieldName("object")
		if objectNode.IsNull() {
			return phpRouteCallCtx{}, false
		}

		methodName := strings.TrimSpace(nameNode.Content(a.content))
		switch methodName {
		case "generate":
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
		case "generateUrl", "redirectToRoute":
			if !isThisVariable(objectNode, a.content) {
				return phpRouteCallCtx{}, false
			}
			if !a.classExtendsAbstractController(callNode) {
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

func isThisVariable(node sitter.Node, content []byte) bool {
	if node.IsNull() || node.Type() != "variable_name" {
		return false
	}
	return strings.TrimSpace(node.Content(content)) == "$this"
}

func (a *phpAnalyzer) propertyHasRouterType(name string) bool {
	if len(a.index.Properties) == 0 {
		return false
	}
	entries, ok := a.index.Properties[name]
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

func (a *phpAnalyzer) classExtendsAbstractController(node sitter.Node) bool {
	target := strings.ToLower(normalizeFQN(abstractControllerFQN))
	if target == "" {
		return false
	}
	for cur := node; !cur.IsNull(); cur = cur.Parent() {
		if cur.Type() != "class_declaration" {
			continue
		}
		info, ok := a.index.Classes[uint32(cur.StartByte())]
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
		return strings.TrimSpace(nameNode.Content(a.content))
	}
	return fmt.Sprintf("anonymous@%d", int(node.StartPoint().Row)+1)
}

func (a *phpAnalyzer) variableHasRouterType(funcName, varName string, line int) bool {
	if funcName == "" || varName == "" {
		return false
	}
	scope, ok := a.index.Variables[funcName]
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
	return php.VariableNameFromNode(node, a.content)
}
