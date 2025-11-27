package translations

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/tliron/commonlog"
	protocol "github.com/tliron/glsp/protocol_3_16"
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
		parseYamlFile(resource, domain, translations)
	}

	return translations
}

func parseYamlFile(path string, domain string, translations TranslationMap) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0

	// Simple stack to keep track of nested keys
	// This is a very basic YAML parser that assumes standard indentation
	type stackItem struct {
		indent int
		key    string
	}
	stack := []stackItem{}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimLeft(line, " ")

		// Skip comments and empty lines
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			lineNumber++
			continue
		}

		indent := len(line) - len(trimmed)

		// Parse key
		parts := strings.SplitN(trimmed, ":", 2)
		key := strings.TrimSpace(parts[0])
		// Remove quotes if present
		key = strings.Trim(key, `"'`)

		// Manage stack based on indentation
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}

		// Build full key
		fullKey := key
		if len(stack) > 0 {
			fullKey = stack[len(stack)-1].key + "." + key
		}

		// Add to stack
		stack = append(stack, stackItem{indent: indent, key: fullKey})

		// If it has a value, it's a leaf node (mostly)
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {

			// Store location
			loc := TranslationLocation{
				URI: "file://" + path,
				Range: protocol.Range{
					Start: protocol.Position{Line: uint32(lineNumber), Character: 0},
					End:   protocol.Position{Line: uint32(lineNumber), Character: uint32(len(line))},
				},
			}

			translations[fullKey] = append(translations[fullKey], loc)
		}

		lineNumber++
	}
}
