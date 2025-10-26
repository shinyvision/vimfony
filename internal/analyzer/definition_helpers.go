package analyzer

import (
	"strings"

	"github.com/shinyvision/vimfony/internal/config"
	php "github.com/shinyvision/vimfony/internal/php"
	"github.com/shinyvision/vimfony/internal/utils"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func normalizeFQN(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\\\", "\\"))
	name = strings.TrimLeft(name, "?\\")
	return name
}

func resolveClassLocations(className string, container *config.ContainerConfig, psr4 config.Psr4Map) ([]protocol.Location, bool) {
	if container == nil || len(psr4) == 0 {
		return nil, false
	}
	className = normalizeFQN(className)
	if className == "" {
		return nil, false
	}
	target, classRange, ok := php.Resolve(className, psr4, container.WorkspaceRoot)
	if !ok {
		return nil, false
	}
	loc := protocol.Location{
		URI:   protocol.DocumentUri(utils.PathToURI(target)),
		Range: classRange,
	}
	return []protocol.Location{loc}, true
}

func resolveServiceIDLocations(serviceID string, container *config.ContainerConfig, psr4 config.Psr4Map) ([]protocol.Location, bool) {
	if container == nil {
		return nil, false
	}
	className, ok := container.ResolveServiceId(serviceID)
	if !ok {
		return nil, false
	}
	return resolveClassLocations(className, container, psr4)
}

func lineAt(content string, line int) (string, bool) {
	if line < 0 {
		return "", false
	}
	currentLine := 0
	start := 0
	for i := 0; i <= len(content); i++ {
		if i == len(content) || content[i] == '\n' {
			if currentLine == line {
				return content[start:i], true
			}
			start = i + 1
			currentLine++
		}
	}
	return "", false
}

func extractIdentifier(line string, offset int, allowed func(rune) bool) (string, int, int, bool) {
	if offset < 0 {
		offset = 0
	}

	runes := []rune(line)
	if offset > len(runes) {
		offset = len(runes)
	}

	left := offset
	for left > 0 && allowed(runes[left-1]) {
		left--
	}

	right := offset
	for right < len(runes) && allowed(runes[right]) {
		right++
	}

	if left == right {
		return "", 0, 0, false
	}

	return string(runes[left:right]), left, right, true
}

func trimQuotes(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func isServiceIdentifierRune(r rune) bool {
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '_', '.', '-', '\\':
		return true
	default:
		return false
	}
}

func isServiceIdentifierWithAtRune(r rune) bool {
	if r == '@' {
		return true
	}
	return isServiceIdentifierRune(r)
}
