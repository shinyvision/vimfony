package analyzer

import (
	"regexp"
	"sort"
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type yamlAnalyzer struct {
	lines     []string
	container *config.ContainerConfig
}

func NewYamlAnalyzer() Analyzer {
	return &yamlAnalyzer{}
}

func (a *yamlAnalyzer) Changed(code []byte, change *sitter.InputEdit) error {
	a.lines = strings.Split(string(code), "\n")
	return nil
}

func (a *yamlAnalyzer) Close() {
	a.lines = nil
}

func (a *yamlAnalyzer) SetContainerConfig(container *config.ContainerConfig) {
	a.container = container
}

func (a *yamlAnalyzer) hasServicePrefix(pos protocol.Position) (bool, string) {
	if int(pos.Line) >= len(a.lines) {
		return false, ""
	}

	line := a.lines[pos.Line]
	if int(pos.Character) > len(line) {
		return false, ""
	}

	re := regexp.MustCompile(`services\:\s*([a-zA-Z0-9_.-]*)$`)
	matches := re.FindStringSubmatch(line[:pos.Character])
	if len(matches) > 1 {
		return true, matches[1]
	}

	re2 := regexp.MustCompile(`['"]@([a-zA-Z0-9_.-]*)'`)
	allMatches := re2.FindAllStringSubmatch(line, -1)
	for _, match := range allMatches {
		if len(match) > 1 {
			return true, match[1]
		}
	}

	return false, ""
}

func (a *yamlAnalyzer) OnCompletion(pos protocol.Position) ([]protocol.CompletionItem, error) {
	if a.container == nil {
		return nil, nil
	}

	found, prefix := a.hasServicePrefix(pos)
	if !found {
		return nil, nil
	}

	return a.serviceCompletionItems(prefix), nil
}

func (a *yamlAnalyzer) serviceCompletionItems(prefix string) []protocol.CompletionItem {
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
