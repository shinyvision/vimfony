package analyzer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shinyvision/vimfony/internal/config"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func makeRouteNameCompletionItems(routes config.RoutesMap, prefix string) []protocol.CompletionItem {
	if len(routes) == 0 {
		return nil
	}

	items := make([]protocol.CompletionItem, 0, len(routes))
	kind := protocol.CompletionItemKindConstant
	detail := "Symfony route"

	for name, route := range routes {
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		documentation := protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: buildRouteDocumentation(name, route.Parameters),
		}

		items = append(items, protocol.CompletionItem{
			Label:         name,
			Kind:          &kind,
			Detail:        &detail,
			Documentation: documentation,
		})
	}

	sortCompletionItemsByShortLex(items)
	return items
}

func makeRouteParameterCompletionItems(routes config.RoutesMap, routeName, prefix string) []protocol.CompletionItem {
	if len(routes) == 0 {
		return nil
	}

	route, ok := routes[routeName]
	if !ok || len(route.Parameters) == 0 {
		return nil
	}

	items := make([]protocol.CompletionItem, 0, len(route.Parameters))
	kind := protocol.CompletionItemKindProperty

	for _, param := range route.Parameters {
		if !strings.HasPrefix(param, prefix) {
			continue
		}
		detail := fmt.Sprintf("parameter for route %s", routeName)
		items = append(items, protocol.CompletionItem{
			Label:  param,
			Kind:   &kind,
			Detail: &detail,
		})
	}

	sortCompletionItemsByShortLex(items)
	return items
}

func buildRouteDocumentation(name string, params []string) string {
	var b strings.Builder
	b.WriteString("**Route:** `")
	b.WriteString(name)
	b.WriteString("`\n\n")

	if len(params) == 0 {
		b.WriteString("*No parameters*")
		return b.String()
	}

	b.WriteString("**Parameters:**\n")
	for _, param := range params {
		b.WriteString("- `")
		b.WriteString(param)
		b.WriteString("`\n")
	}
	return b.String()
}

func sortCompletionItemsByShortLex(items []protocol.CompletionItem) {
	sort.Slice(items, func(i, j int) bool {
		li, lj := items[i].Label, items[j].Label
		if len(li) != len(lj) {
			return len(li) < len(lj)
		}
		return li < lj
	})
}
