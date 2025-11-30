package analyzer

import (
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	php "github.com/shinyvision/vimfony/internal/php"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func (a *phpAnalyzer) translationCompletionItems(pos protocol.Position) []protocol.CompletionItem {
	found, prefix := a.isTypingTranslationKey(pos)
	if !found {
		return nil
	}

	items := make([]protocol.CompletionItem, 0, len(a.container.TranslationKeys))
	kind := protocol.CompletionItemKindText

	for key := range a.container.TranslationKeys {
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}

		label := key
		items = append(items, protocol.CompletionItem{
			Label:  label,
			Kind:   &kind,
			Detail: &label,
		})
	}

	return items
}

func (a *phpAnalyzer) isTypingTranslationKey(pos protocol.Position) (bool, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	ctx, ok := a.translationContextAt(pos)
	if !ok {
		return false, ""
	}
	return true, a.stringPrefix(ctx.strNode, pos)
}

func (a *phpAnalyzer) translationContextAt(pos protocol.Position) (phpCallCtx, bool) {
	if a.doc == nil {
		return phpCallCtx{}, false
	}

	node, content, index, ok := a.doc.GetNodeAt(pos)
	if !ok {
		return phpCallCtx{}, false
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
			continue
		}

		argIndex := -1
		for i := uint32(0); i < argsNode.NamedChildCount(); i++ {
			if argsNode.NamedChild(i).Equal(argNode) {
				argIndex = int(i)
				break
			}
		}
		if argIndex != 0 { // Translation key is usually the first argument
			return phpCallCtx{}, false
		}

		callNode := argsNode.Parent()
		for !callNode.IsNull() && callNode.Type() != "member_call_expression" {
			callNode = callNode.Parent()
		}
		if callNode.IsNull() || callNode.Type() != "member_call_expression" {
			return phpCallCtx{}, false
		}

		nameNode := callNode.ChildByFieldName("name")
		if nameNode.IsNull() {
			return phpCallCtx{}, false
		}

		objectNode := callNode.ChildByFieldName("object")
		if objectNode.IsNull() {
			return phpCallCtx{}, false
		}

		methodName := strings.TrimSpace(nameNode.Content(content))
		if methodName != "trans" {
			return phpCallCtx{}, false
		}

		// Check if object is a translator or $this in controller
		callLine := int(callNode.StartPoint().Row) + 1

		if isThisVariable(objectNode, content) {
			// Check if class extends AbstractController
			controllerTarget := strings.ToLower(normalizeFQN(abstractControllerFQN))
			if controllerTarget != "" && classExtendsAbstractControllerIndex(index, callNode, controllerTarget) {
				if str.IsNull() {
					return phpCallCtx{}, false
				}
				return phpCallCtx{
					callNode: callNode,
					argsNode: argsNode,
					argIndex: argIndex,
					strNode:  str,
				}, true
			}
		}

		// Check if variable is a translator
		varName := php.VariableNameFromNode(objectNode, content)
		if varName != "" {
			funcName := a.enclosingFunctionName(callNode)
			if funcName != "" {
				if variableHasTranslatorTypeIndex(index, funcName, varName, callLine) {
					if str.IsNull() {
						return phpCallCtx{}, false
					}
					return phpCallCtx{
						callNode: callNode,
						argsNode: argsNode,
						argIndex: argIndex,
						strNode:  str,
					}, true
				}
			}
		}

		// Also check property access $this->translator
		propertyName := thisPropertyNameFromMemberAccessContent(content, objectNode)
		if propertyName != "" {
			if propertyHasTranslatorTypeIndex(index, propertyName) {
				if str.IsNull() {
					return phpCallCtx{}, false
				}
				return phpCallCtx{
					callNode: callNode,
					argsNode: argsNode,
					argIndex: argIndex,
					strNode:  str,
				}, true
			}
		}

		return phpCallCtx{}, false
	}

	return phpCallCtx{}, false
}

func (a *phpAnalyzer) resolveTranslationDefinition(pos protocol.Position) ([]protocol.Location, bool) {
	a.mu.RLock()
	container := a.container
	a.mu.RUnlock()

	if container == nil {
		return nil, false
	}

	ctx, ok := a.translationContextAt(pos)
	if !ok {
		return nil, false
	}

	key := a.stringContent(ctx.strNode)
	if key == "" {
		return nil, false
	}

	locs, ok := container.TranslationKeys[key]
	if !ok || len(locs) == 0 {
		return nil, false
	}

	// Filter by DefaultLocale if set
	if container.DefaultLocale != "" {
		var defaultLocaleLocs []protocol.Location
		for _, loc := range locs {
			parts := strings.Split(loc.URI, ".")
			if len(parts) >= 3 {
				locale := parts[len(parts)-2]
				if locale == container.DefaultLocale {
					defaultLocaleLocs = append(defaultLocaleLocs, protocol.Location{
						URI:   protocol.DocumentUri(loc.URI),
						Range: loc.Range,
					})
				}
			}
		}
		if len(defaultLocaleLocs) > 0 {
			return defaultLocaleLocs, true
		}
	}

	// If no default locale matches, return all locations
	result := make([]protocol.Location, len(locs))
	for i, loc := range locs {
		result[i] = protocol.Location{
			URI:   protocol.DocumentUri(loc.URI),
			Range: loc.Range,
		}
	}
	return result, true
}

func canonicalTranslatorType(name string) (string, bool) {
	normalized := normalizeFQN(name)
	if normalized == "" {
		return "", false
	}

	targets := []string{translatorInterfaceFQN, legacyTranslatorFQN}
	for _, target := range targets {
		target = normalizeFQN(target)
		if strings.EqualFold(normalized, target) {
			return target, true
		}
		if strings.EqualFold(shortName(normalized), shortName(target)) {
			return target, true
		}
	}

	return "", false
}

func variableHasTranslatorTypeIndex(index php.IndexedTree, funcName, varName string, line int) bool {
	return variableHasTypeIndex(index, funcName, varName, line, canonicalTranslatorType)
}

func propertyHasTranslatorTypeIndex(index php.IndexedTree, name string) bool {
	return propertyHasTypeIndex(index, name, canonicalTranslatorType)
}
