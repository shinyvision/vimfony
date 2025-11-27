package analyzer

import (
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func (a *twigAnalyzer) translationCompletionItems(pos protocol.Position) []protocol.CompletionItem {
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

func (a *twigAnalyzer) resolveTranslationDefinition(pos protocol.Position) ([]protocol.Location, bool) {
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
			// Extract locale from URI
			// URI is file:///path/to/domain.locale.format
			// We assume standard Symfony naming convention
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

func (a *twigAnalyzer) isTypingTranslationKey(pos protocol.Position) (bool, string) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	ctx, ok := a.translationContextAt(pos)
	if !ok {
		return false, ""
	}
	return true, a.stringPrefix(ctx.strNode, pos)
}

func (a *twigAnalyzer) translationContextAt(pos protocol.Position) (twigCallCtx, bool) {
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

	node := root.NamedDescendantForPointRange(point, point)
	if node.IsNull() {
		return twigCallCtx{}, false
	}

	// Look for string literal
	var str sitter.Node
	for cur := node; !cur.IsNull(); cur = cur.Parent() {
		if str.IsNull() && cur.Type() == "string" {
			str = cur
			continue
		}

		if str.IsNull() {
			continue
		}

		// Check for filter usage in output_directive: {{ 'key'|trans }}
		if cur.Type() == "output_directive" || cur.Type() == "filter_expression" {
			// Check if there is a filter named 'trans' or 't'
			hasTransFilter := false
			for i := uint32(0); i < cur.NamedChildCount(); i++ {
				child := cur.NamedChild(i)
				if child.Type() == "filter" {
					nameNode := child.NamedChild(0)
					if !nameNode.IsNull() {
						name := strings.TrimSpace(string(a.content[nameNode.StartByte():nameNode.EndByte()]))
						if name == "trans" || name == "t" {
							hasTransFilter = true
							break
						}
					}
				}
			}

			if hasTransFilter {
				// Check if str is the expression being filtered.
				// We assume the first named child is the expression.
				first := cur.NamedChild(0)
				if !first.IsNull() && first.Equal(str) {
					return twigCallCtx{strNode: str}, true
				}
			}
		}
	}

	return twigCallCtx{}, false
}
