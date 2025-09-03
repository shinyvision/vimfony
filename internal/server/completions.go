package server

import (
	"sort"
	"strings"

	"github.com/shinyvision/vimfony/internal/analyzer"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func (s *Server) onCompletion(_ *glsp.Context, p *protocol.CompletionParams) (any, error) {
	doc, ok := s.state.GetDocument(p.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	var (
		found  bool
		prefix string
	)

	switch doc.LanguageID {
	// TODO: We'll create an analyzer for each file (including twig). Much more reliable than string matching.
	case "yaml":
		found, prefix = doc.HasServicePrefix(p.Position)
	case "php":
		if doc.Analyzer != nil {
			if pa, ok := doc.Analyzer.(analyzer.PhpAnalyzer); ok {
				if f, p := pa.IsInAutoconfigure(p.Position); f && strings.HasPrefix(p, "@") {
					found = f
					prefix = strings.TrimPrefix(p, "@")
				}
			}
		}
	case "xml":
		if doc.Analyzer != nil {
			if pa, ok := doc.Analyzer.(analyzer.XmlAnalyzer); ok {
				if f, p := pa.IsInServiceIDAttribute(p.Position); f {
					found = f
					prefix = p
				}
			}
		}
	}

	if !found {
		return nil, nil
	}

	return s.resolveCompletionItems(prefix), nil
}

func (s *Server) resolveCompletionItems(prefix string) []protocol.CompletionItem {
	items := []protocol.CompletionItem{}
	seen := make(map[string]bool)
	kind := protocol.CompletionItemKindKeyword
	for id, class := range s.config.Container.ServiceClasses {
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

	for alias, serviceId := range s.config.Container.ServiceAliases {
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
		refI := s.config.Container.ServiceReferences[idI]
		refJ := s.config.Container.ServiceReferences[idJ]

		if refI != refJ {
			return refI > refJ
		}
		return idI < idJ
	})

	return items
}
