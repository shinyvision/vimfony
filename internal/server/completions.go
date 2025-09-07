package server

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shinyvision/vimfony/internal/analyzer"
	"github.com/shinyvision/vimfony/internal/state"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func (s *Server) onCompletion(_ *glsp.Context, p *protocol.CompletionParams) (any, error) {
	doc, ok := s.state.GetDocument(p.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	serviceCompletions := s.onServiceCompletions(doc, p)
	if len(serviceCompletions) > 0 {
		return serviceCompletions, nil
	}

	twigFunctionCompletions := s.onTwigFunctionCompletions(doc, p)
	if len(twigFunctionCompletions) > 0 {
		return twigFunctionCompletions, nil
	}

	return nil, nil
}

func (s *Server) onTwigFunctionCompletions(doc *state.Document, p *protocol.CompletionParams) []protocol.CompletionItem {
	if doc.LanguageID != "twig" {
		return nil
	}

	var (
		found  bool
		prefix string
	)

	if doc.Analyzer != nil {
		if ta, ok := doc.Analyzer.(analyzer.TwigAnalyzer); ok {
			if f, p := ta.IsTypingFunction(p.Position); f {
				found = f
				prefix = p
			}
		}
	}

	if !found {
		return nil
	}

	return s.twigFunctionCompletionItems(prefix)
}

func (s *Server) twigFunctionCompletionItems(prefix string) []protocol.CompletionItem {
	items := []protocol.CompletionItem{}
	kind := protocol.CompletionItemKindFunction

	for name := range s.config.Container.TwigFunctions {
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

func (s *Server) onServiceCompletions(doc *state.Document, p *protocol.CompletionParams) []protocol.CompletionItem {
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
		return nil
	}

	return s.serviceCompletionItems(prefix)
}

func (s *Server) serviceCompletionItems(prefix string) []protocol.CompletionItem {
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
