package analyzer

import (
	"regexp"
	"sort"
	"strings"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/twig"
	"github.com/shinyvision/vimfony/internal/utils"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type yamlAnalyzer struct {
	lines     []string
	content   string
	container *config.ContainerConfig
	autoload  config.AutoloadMap
}

func NewYamlAnalyzer() Analyzer {
	return &yamlAnalyzer{}
}

func (a *yamlAnalyzer) Changed(code []byte, change *sitter.InputEdit) error {
	a.content = string(code)
	a.lines = strings.Split(a.content, "\n")
	return nil
}

func (a *yamlAnalyzer) Close() {
	a.lines = nil
	a.content = ""
}

func (a *yamlAnalyzer) SetContainerConfig(container *config.ContainerConfig) {
	a.container = container
}

func (a *yamlAnalyzer) SetPsr4Map(psr4 *config.AutoloadMap) {
	if psr4 == nil {
		a.autoload = config.AutoloadMap{}
		return
	}
	a.autoload = *psr4
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

func (a *yamlAnalyzer) OnDefinition(pos protocol.Position) ([]protocol.Location, error) {
	if a.container == nil {
		return nil, nil
	}

	if twigPath, ok := twig.PathAt(a.content, pos); ok {
		if target, ok := twig.Resolve(twigPath, a.container); ok {
			loc := protocol.Location{
				URI:   protocol.DocumentUri(utils.PathToURI(target)),
				Range: protocol.Range{},
			}
			return []protocol.Location{loc}, nil
		}
	}

	line, ok := lineAt(a.content, int(pos.Line))
	if !ok || line == "" {
		return nil, nil
	}

	token, _, _, ok := extractIdentifier(line, int(pos.Character), isServiceIdentifierWithAtRune)
	if !ok {
		return nil, nil
	}
	token = trimQuotes(strings.TrimSpace(token))
	if token == "" {
		return nil, nil
	}

	if strings.HasPrefix(token, "@") {
		serviceID := strings.TrimPrefix(token, "@")
		if locs, ok := resolveServiceIDLocations(serviceID, a.container, a.autoload); ok {
			return locs, nil
		}
		// fall through to consider remainder for classes or aliases without '@'
		token = serviceID
	}

	if strings.Contains(token, "\\") {
		if locs, ok := resolveClassLocations(token, a.container, a.autoload); ok {
			return locs, nil
		}
	}

	if locs, ok := resolveServiceIDLocations(token, a.container, a.autoload); ok {
		return locs, nil
	}

	return nil, nil
}
