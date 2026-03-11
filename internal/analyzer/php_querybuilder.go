package analyzer

import (
	"regexp"
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/doctrine"
	"github.com/shinyvision/vimfony/internal/php"
	"github.com/shinyvision/vimfony/internal/utils"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

var qbMethods = map[string]bool{
	"select": true, "addSelect": true,
	"where": true, "andWhere": true, "orWhere": true,
	"orderBy": true, "addOrderBy": true,
	"groupBy": true, "addGroupBy": true,
	"having": true, "andHaving": true, "orHaving": true,
	"set": true,
}

var qbJoinMethods = map[string]bool{
	"join": true, "innerJoin": true, "leftJoin": true,
}

var qbAliasRegex = regexp.MustCompile(`([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]*)$`)
var qbFieldRegex = regexp.MustCompile(`([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]+)`)

func (a *phpAnalyzer) queryBuilderCompletionItems(pos protocol.Position) []protocol.CompletionItem {
	found, alias, prefix, isJoin, callNode := a.isTypingQueryBuilderProperty(pos)
	if !found || alias == "" {
		return nil
	}

	entityFQN := a.resolveQueryBuilderAliasEntity(alias, callNode)
	if entityFQN == "" {
		return nil
	}

	return a.entityPropertyCompletionItems(entityFQN, prefix, isJoin)
}

func (a *phpAnalyzer) isTypingQueryBuilderProperty(pos protocol.Position) (bool, string, string, bool, sitter.Node) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.doc == nil {
		return false, "", "", false, sitter.Node{}
	}

	node, content, _, ok := a.doc.GetNodeAt(pos)
	if !ok {
		return false, "", "", false, sitter.Node{}
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

		argsNode := cur.Parent()
		if argsNode.IsNull() || argsNode.Type() != "arguments" {
			continue
		}

		callNode := argsNode.Parent()
		for !callNode.IsNull() && callNode.Type() != "member_call_expression" {
			callNode = callNode.Parent()
		}
		if callNode.IsNull() || callNode.Type() != "member_call_expression" {
			continue
		}

		nameNode := callNode.ChildByFieldName("name")
		if nameNode.IsNull() {
			continue
		}

		methodName := strings.TrimSpace(nameNode.Content(content))
		if !qbMethods[methodName] && !qbJoinMethods[methodName] {
			continue
		}

		if str.IsNull() {
			continue
		}

		typedContent := a.stringPrefix(str, pos)
		if typedContent == "" {
			continue
		}

		matches := qbAliasRegex.FindStringSubmatch(typedContent)
		if len(matches) == 3 {
			return true, matches[1], matches[2], qbJoinMethods[methodName], callNode
		}
		return false, "", "", false, sitter.Node{}
	}

	return false, "", "", false, sitter.Node{}
}

func walkSitterTreePostOrder(node sitter.Node, cb func(n sitter.Node)) {
	if node.IsNull() {
		return
	}
	for i := uint32(0); i < node.NamedChildCount(); i++ {
		walkSitterTreePostOrder(node.NamedChild(i), cb)
	}
	cb(node)
}

func (a *phpAnalyzer) resolveQueryBuilderAliasEntity(targetAlias string, callNode sitter.Node) string {
	a.mu.RLock()
	doc := a.doc
	autoload := a.autoload
	container := a.container
	doctrineReg := a.doctrine
	a.mu.RUnlock()

	if doc == nil || container == nil {
		return ""
	}

	var content string
	var currentClass string
	var currentNamespace string
	var currentUses map[string]string

	doc.Read(func(tree *sitter.Tree, docContent []byte, index php.IndexedTree) {
		content = string(docContent)
		if tree != nil {
			for _, cls := range index.Classes {
				currentClass = cls.Name
				currentNamespace = cls.Namespace
				break
			}
		}
		currentUses = index.Uses
	})

	var funcNode sitter.Node
	for cur := callNode; !cur.IsNull(); cur = cur.Parent() {
		if cur.Type() == "method_declaration" || cur.Type() == "function_definition" {
			funcNode = cur
			break
		}
	}

	if funcNode.IsNull() {
		return ""
	}

	aliasMap := make(map[string]string)

	// Post-order: inner chained calls (createQueryBuilder) register aliases
	// before outer calls (join) that depend on them.
	walkSitterTreePostOrder(funcNode, func(n sitter.Node) {
		if n.Type() != "member_call_expression" {
			return
		}

		nameNode := n.ChildByFieldName("name")
		if nameNode.IsNull() {
			return
		}
		methodName := strings.TrimSpace(nameNode.Content([]byte(content)))

		argsNode := n.ChildByFieldName("arguments")
		if argsNode.IsNull() {
			return
		}

		if methodName == "from" {
			if argsNode.NamedChildCount() >= 2 {
				clsArg := argsNode.NamedChild(0)
				aliasArg := argsNode.NamedChild(1)

				clsName := a.resolveClassArgToFQN(clsArg, content, currentNamespace, currentUses)
				aliasStr := a.extractStringArg(aliasArg, content)

				if clsName != "" && aliasStr != "" {
					aliasMap[aliasStr] = clsName
				}
			}
		} else if methodName == "createQueryBuilder" {
			if argsNode.NamedChildCount() >= 1 {
				aliasArg := argsNode.NamedChild(0)
				aliasStr := a.extractStringArg(aliasArg, content)
				if aliasStr != "" {
					objectNode := n.ChildByFieldName("object")
					guessed := false

					if !objectNode.IsNull() && objectNode.Type() == "member_call_expression" {
						objNameNode := objectNode.ChildByFieldName("name")
						if !objNameNode.IsNull() && strings.TrimSpace(objNameNode.Content([]byte(content))) == "getRepository" {
							objArgs := objectNode.ChildByFieldName("arguments")
							if !objArgs.IsNull() && objArgs.NamedChildCount() >= 1 {
								clsArg := objArgs.NamedChild(0)
								clsName := a.resolveClassArgToFQN(clsArg, content, currentNamespace, currentUses)
								if clsName != "" {
									aliasMap[aliasStr] = clsName
									guessed = true
								}
							}
						}
					}

					if !guessed && strings.HasSuffix(currentClass, "Repository") {
						entityName := strings.TrimSuffix(currentClass, "Repository")
						// usually Entity namespace is same up to /Repository/ with /Entity/
						guessFQN := strings.Replace(currentNamespace, "\\Repository", "\\Entity", 1) + "\\" + entityName
						aliasMap[aliasStr] = guessFQN
					}
				}
			}
		} else if qbJoinMethods[methodName] {
			if argsNode.NamedChildCount() >= 2 {
				joinArg := argsNode.NamedChild(0)
				aliasArg := argsNode.NamedChild(1)

				joinStr := a.extractStringArg(joinArg, content)
				aliasStr := a.extractStringArg(aliasArg, content)

				if joinStr != "" && aliasStr != "" {
					parts := strings.Split(joinStr, ".")
					if len(parts) == 2 {
						parentAlias := parts[0]
						relation := parts[1]

						parentFQN, ok := aliasMap[parentAlias]
						if ok {
							var relationTypeFQN string
							if doctrineReg != nil {
								relationTypeFQN = doctrineReg.AssociationTargetEntity(parentFQN, relation)
							}
							if relationTypeFQN == "" {
								relationTypeFQN = a.resolvePropertyType(parentFQN, relation, autoload)
							}
							if relationTypeFQN != "" {
								aliasMap[aliasStr] = relationTypeFQN
							}
						}
					}
				}
			}
		}
	})

	return aliasMap[targetAlias]
}

func (a *phpAnalyzer) extractStringArg(arg sitter.Node, content string) string {
	val := arg
	if val.Type() == "argument" && val.NamedChildCount() > 0 {
		val = val.NamedChild(0)
	}
	if val.Type() == "string" {
		strContent := val.Content([]byte(content))
		if len(strContent) >= 2 {
			return strContent[1 : len(strContent)-1]
		}
	}
	return ""
}

func (a *phpAnalyzer) resolveClassArgToFQN(arg sitter.Node, content string, namespace string, uses map[string]string) string {
	val := arg
	if val.Type() == "argument" && val.NamedChildCount() > 0 {
		val = val.NamedChild(0)
	}
	if val.Type() == "class_constant_access_expression" {
		if val.NamedChildCount() >= 2 {
			clsNode := val.NamedChild(0)
			nameNode := val.NamedChild(val.NamedChildCount() - 1)

			name := strings.TrimSpace(nameNode.Content([]byte(content)))
			if name == "class" || name == "CLASS" {
				clsName := strings.TrimSpace(clsNode.Content([]byte(content)))
				return a.resolveToFQN(clsName, namespace, uses)
			}
		}
	} else if val.Type() == "string" {
		str := a.extractStringArg(val, content)
		return str
	}
	return ""
}

func (a *phpAnalyzer) resolveToFQN(name string, namespace string, uses map[string]string) string {
	if strings.HasPrefix(name, "\\") {
		return name
	}
	if fqn, ok := uses[name]; ok {
		return fqn
	}
	if namespace != "" {
		return namespace + "\\" + name
	}
	return name
}

func (a *phpAnalyzer) resolvePropertyType(classFQN string, property string, autoload config.AutoloadMap) string {
	a.mu.RLock()
	container := a.container
	store := a.docStore
	a.mu.RUnlock()

	locs, ok := resolveClassLocations(classFQN, container, autoload, store)
	if !ok || len(locs) == 0 {
		return ""
	}

	uri := locs[0].URI
	path := utils.UriToPath(string(uri))

	doc, err := store.Get(path)
	if err != nil || doc == nil {
		return ""
	}

	var propType []php.TypeOccurrence
	var classUses map[string]string
	var classNamespace string

	doc.Read(func(_ *sitter.Tree, _ []byte, index php.IndexedTree) {
		propType = index.Properties[property]
		classUses = index.Uses
		for _, cls := range index.Classes {
			classNamespace = cls.Namespace
			break
		}
	})

	if len(propType) > 0 {
		for _, occ := range propType {
			if occ.Type != "" && occ.Type != "mixed" {
				return a.resolveToFQN(occ.Type, classNamespace, classUses)
			}
		}
	}
	return ""
}

type propertySource struct {
	occs     []php.TypeOccurrence
	ownerFQN string
	srcLines []string
}

func (a *phpAnalyzer) entityPropertyCompletionItems(entityFQN string, prefix string, associationsOnly bool) []protocol.CompletionItem {
	a.mu.RLock()
	autoload := a.autoload
	container := a.container
	store := a.docStore
	doctrineReg := a.doctrine
	a.mu.RUnlock()

	if doctrineReg == nil {
		return nil
	}

	mappedFields := doctrineReg.MappedFields(entityFQN)
	if len(mappedFields) == 0 {
		return nil
	}

	propSources := make(map[string]*propertySource)
	if store != nil {
		locs, ok := resolveClassLocations(entityFQN, container, autoload, store)
		if ok && len(locs) > 0 {
			path := utils.UriToPath(string(locs[0].URI))
			if doc, err := store.Get(path); err == nil && doc != nil {
				doc.Read(func(_ *sitter.Tree, content []byte, index php.IndexedTree) {
					lines := strings.Split(string(content), "\n")
					for k, v := range index.Properties {
						propSources[k] = &propertySource{
							occs:     append([]php.TypeOccurrence(nil), v...),
							ownerFQN: entityFQN,
							srcLines: lines,
						}
					}
				})
			}
		}
	}

	items := make([]protocol.CompletionItem, 0, len(mappedFields))
	kind := protocol.CompletionItemKindProperty

	for _, mf := range mappedFields {
		if associationsOnly && mf.Kind != doctrine.FieldKindAssociation {
			continue
		}
		if prefix != "" && !strings.HasPrefix(mf.Name, prefix) {
			continue
		}

		detail := entityFQN + "::$" + mf.Name + " (" + string(mf.Kind) + ")"
		if ps, ok := propSources[mf.Name]; ok {
			detail = ps.ownerFQN + "::$" + mf.Name + " (" + string(mf.Kind) + ")"
		}

		var documentation protocol.MarkupContent
		if ps, ok := propSources[mf.Name]; ok {
			documentation = protocol.MarkupContent{
				Kind:  protocol.MarkupKindMarkdown,
				Value: buildPropertyDocumentation(ps.occs, ps.srcLines),
			}
		}

		items = append(items, protocol.CompletionItem{
			Label:         mf.Name,
			Kind:          &kind,
			Detail:        &detail,
			Documentation: documentation,
		})
	}

	sortCompletionItemsByShortLex(items)
	return items
}

func buildPropertyDocumentation(occs []php.TypeOccurrence, srcLines []string) string {
	propLine := 0
	if len(occs) > 0 {
		propLine = occs[len(occs)-1].Line
	}

	if propLine <= 0 || propLine > len(srcLines) {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("```php\n<?php\n")

	contextLines := extractPropertyContext(srcLines, propLine)
	for _, line := range contextLines {
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	sb.WriteString("```")
	return sb.String()
}

func (a *phpAnalyzer) queryBuilderDefinition(pos protocol.Position) ([]protocol.Location, bool) {
	found, alias, fieldName, callNode := a.queryBuilderFieldAtCursor(pos)
	if !found || alias == "" || fieldName == "" {
		return nil, false
	}

	entityFQN := a.resolveQueryBuilderAliasEntity(alias, callNode)
	if entityFQN == "" {
		return nil, false
	}

	a.mu.RLock()
	store := a.docStore
	doctrineReg := a.doctrine
	a.mu.RUnlock()

	if doctrineReg == nil || store == nil {
		return nil, false
	}

	mappedFields := doctrineReg.MappedFields(entityFQN)
	isMapped := false
	for _, mf := range mappedFields {
		if mf.Name == fieldName {
			isMapped = true
			break
		}
	}
	if !isMapped {
		return nil, false
	}

	loc, ok := a.resolvePropertyLocation(entityFQN, fieldName, store)
	if !ok {
		return nil, false
	}
	return []protocol.Location{loc}, true
}

func (a *phpAnalyzer) queryBuilderFieldAtCursor(pos protocol.Position) (bool, string, string, sitter.Node) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.doc == nil {
		return false, "", "", sitter.Node{}
	}

	node, content, _, ok := a.doc.GetNodeAt(pos)
	if !ok {
		return false, "", "", sitter.Node{}
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

		argsNode := cur.Parent()
		if argsNode.IsNull() || argsNode.Type() != "arguments" {
			continue
		}

		callNode := argsNode.Parent()
		for !callNode.IsNull() && callNode.Type() != "member_call_expression" {
			callNode = callNode.Parent()
		}
		if callNode.IsNull() || callNode.Type() != "member_call_expression" {
			continue
		}

		nameNode := callNode.ChildByFieldName("name")
		if nameNode.IsNull() {
			continue
		}

		methodName := strings.TrimSpace(nameNode.Content(content))
		if !qbMethods[methodName] && !qbJoinMethods[methodName] {
			continue
		}

		if str.IsNull() {
			continue
		}

		sb, eb := int(str.StartByte()), int(str.EndByte())
		if eb-sb < 2 {
			continue
		}
		inner := string(content[sb+1 : eb-1])
		caret := lspPosToByteOffset(content, pos)
		rel := caret - sb - 1
		if rel < 0 || rel > len(inner) {
			continue
		}

		for _, idx := range qbFieldRegex.FindAllStringSubmatchIndex(inner, -1) {
			if rel >= idx[0] && rel <= idx[1] {
				alias := inner[idx[2]:idx[3]]
				field := inner[idx[4]:idx[5]]
				return true, alias, field, callNode
			}
		}
		return false, "", "", sitter.Node{}
	}

	return false, "", "", sitter.Node{}
}

func (a *phpAnalyzer) resolvePropertyLocation(entityFQN, propertyName string, store *php.DocumentStore) (protocol.Location, bool) {
	visited := make(map[string]bool)
	return a.findPropertyInHierarchy(entityFQN, propertyName, store, visited)
}

func (a *phpAnalyzer) findPropertyInHierarchy(classFQN, propertyName string, store *php.DocumentStore, visited map[string]bool) (protocol.Location, bool) {
	fqn := normalizeFQN(classFQN)
	if fqn == "" || visited[strings.ToLower(fqn)] {
		return protocol.Location{}, false
	}
	visited[strings.ToLower(fqn)] = true

	autoload, root := store.Config()
	path, ok := config.AutoloadResolve(fqn, autoload, root)
	if !ok || path == "" {
		return protocol.Location{}, false
	}

	doc, err := store.Get(path)
	if err != nil || doc == nil {
		return protocol.Location{}, false
	}

	var propLine int
	var propCol int
	var extends []string
	target := "$" + propertyName

	doc.Read(func(_ *sitter.Tree, content []byte, index php.IndexedTree) {
		if occs, ok := index.Properties[propertyName]; ok && len(occs) > 0 {
			propLine = occs[len(occs)-1].Line
		}
		for _, cls := range index.Classes {
			extends = cls.Extends
			break
		}
		if propLine > 0 {
			lines := strings.SplitN(string(content), "\n", propLine+1)
			if propLine <= len(lines) {
				col := strings.Index(lines[propLine-1], target)
				if col >= 0 {
					propCol = col
				}
			}
		}
	})

	if propLine > 0 {
		return protocol.Location{
			URI: protocol.DocumentUri(utils.PathToURI(path)),
			Range: protocol.Range{
				Start: protocol.Position{Line: uint32(propLine - 1), Character: uint32(propCol)},
				End:   protocol.Position{Line: uint32(propLine - 1), Character: uint32(propCol + len(target))},
			},
		}, true
	}

	for _, parent := range extends {
		if loc, ok := a.findPropertyInHierarchy(parent, propertyName, store, visited); ok {
			return loc, true
		}
	}

	return protocol.Location{}, false
}

func extractPropertyContext(srcLines []string, propLine int) []string {
	idx := propLine - 1
	if idx < 0 || idx >= len(srcLines) {
		return nil
	}

	defLine := srcLines[idx]
	result := []string{defLine}

	if idx == 0 {
		return result
	}

	prevLine := strings.TrimSpace(srcLines[idx-1])

	if prevLine == "" {
		return result
	}

	if strings.HasSuffix(prevLine, "*/") {
		if strings.HasPrefix(prevLine, "/**") || strings.HasPrefix(prevLine, "/*") {
			result = append([]string{srcLines[idx-1]}, result...)
			return result
		}
		start := idx - 1
		for start > 0 {
			trimmed := strings.TrimSpace(srcLines[start-1])
			start--
			if strings.HasPrefix(trimmed, "/**") || strings.HasPrefix(trimmed, "/*") {
				break
			}
		}
		block := make([]string, 0, idx-start)
		for i := start; i < idx; i++ {
			block = append(block, srcLines[i])
		}
		result = append(block, result...)
		return result
	}

	if strings.HasSuffix(prevLine, "]") {
		start := idx - 1
		for start > 0 {
			trimmed := strings.TrimSpace(srcLines[start-1])
			if strings.HasPrefix(trimmed, "#[") {
				start--
				break
			}
			// Not part of an attribute block, stop
			if !strings.HasPrefix(trimmed, "#[") && !strings.Contains(trimmed, "(") && !strings.Contains(trimmed, ")") && !strings.Contains(trimmed, "]") && !strings.Contains(trimmed, ",") {
				break
			}
			start--
		}
		block := make([]string, 0, idx-start)
		for i := start; i < idx; i++ {
			block = append(block, srcLines[i])
		}
		result = append(block, result...)
		return result
	}

	return result
}
