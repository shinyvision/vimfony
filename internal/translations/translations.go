package translations

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tliron/commonlog"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"
)

type TranslationLocation struct {
	URI   string
	Range protocol.Range
}

type TranslationMap map[string][]TranslationLocation

func Parse(resources []string) TranslationMap {
	logger := commonlog.GetLoggerf("vimfony.translations")
	translations := make(TranslationMap)

	for _, resource := range resources {
		// Only support YAML for now
		if !strings.HasSuffix(resource, ".yaml") && !strings.HasSuffix(resource, ".yml") {
			continue
		}

		// Extract domain from filename (e.g., messages.en.yaml -> messages)
		filename := filepath.Base(resource)
		parts := strings.Split(filename, ".")
		domain := "messages"
		if len(parts) > 0 {
			domain = parts[0]
		}

		logger.Debugf("parsing translation file: %s (domain: %s)", resource, domain)
		parseYamlFile(resource, translations)
	}

	return translations
}

func parseYamlFile(path string, translations TranslationMap) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	var node yaml.Node
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&node); err != nil {
		return
	}

	traverseYamlNode(&node, "", path, translations)
}

func traverseYamlNode(node *yaml.Node, prefix string, path string, translations TranslationMap) {
	if node.Kind == yaml.DocumentNode {
		for _, child := range node.Content {
			traverseYamlNode(child, prefix, path, translations)
		}
		return
	}

	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]

			key := keyNode.Value
			fullKey := key
			if prefix != "" {
				fullKey = prefix + "." + key
			}

			switch valueNode.Kind {
			case yaml.ScalarNode:
				// Corrects for 1-based line / column
				line := uint32(keyNode.Line - 1)
				col := uint32(keyNode.Column - 1)

				loc := TranslationLocation{
					URI: "file://" + path,
					Range: protocol.Range{
						Start: protocol.Position{Line: line, Character: col},
						End:   protocol.Position{Line: line, Character: col + uint32(len(key))},
					},
				}
				translations[fullKey] = append(translations[fullKey], loc)
			case yaml.MappingNode:
				traverseYamlNode(valueNode, fullKey, path, translations)
			}
		}
	}
}
