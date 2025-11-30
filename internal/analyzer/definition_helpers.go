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

func resolveClassLocations(className string, container *config.ContainerConfig, autoload config.AutoloadMap, store *php.DocumentStore) ([]protocol.Location, bool) {
	if container == nil || autoload.IsEmpty() || store == nil {
		return nil, false
	}
	className = normalizeFQN(className)
	if className == "" {
		return nil, false
	}
	target, classRange, ok := php.Resolve(store, className)
	if !ok {
		return nil, false
	}
	loc := protocol.Location{
		URI:   protocol.DocumentUri(utils.PathToURI(target)),
		Range: classRange,
	}
	return []protocol.Location{loc}, true
}

func resolveServiceIDLocations(serviceID string, container *config.ContainerConfig, autoload config.AutoloadMap, store *php.DocumentStore) ([]protocol.Location, bool) {
	if container == nil {
		return nil, false
	}
	className, ok := container.ResolveServiceId(serviceID)
	if !ok {
		return nil, false
	}
	return resolveClassLocations(className, container, autoload, store)
}

func resolveRouteLocations(route config.Route, uri string, doc *php.Document) []protocol.Location {
	if doc == nil {
		return nil
	}

	method := route.Action
	if method == "" {
		method = "__invoke"
	}

	index := doc.Index()
	if len(index.PublicFunctions) == 0 {
		return nil
	}

	candidates := []string{method}
	if method != "__invoke" {
		candidates = append(candidates, "__invoke")
	}

	for _, candidate := range candidates {
		target := "::" + candidate
		for _, publicMethod := range index.PublicFunctions {
			if !strings.HasSuffix(publicMethod.Name, target) {
				continue
			}
			if rng, ok := lineColumnRangeToProtocol(publicMethod.Range); ok {
				resultURI := publicMethod.URI
				if resultURI == "" {
					resultURI = uri
				}
				if resultURI == "" {
					continue
				}
				return []protocol.Location{{
					URI:   protocol.DocumentUri(resultURI),
					Range: rng,
				}}
			}
		}
	}

	return nil
}

func routeDocument(route config.Route, container *config.ContainerConfig, autoload config.AutoloadMap, store *php.DocumentStore) (*php.Document, string, bool) {
	if store == nil || container == nil || autoload.IsEmpty() {
		return nil, "", false
	}
	controllerID := route.Controller
	if controllerID == "" {
		return nil, "", false
	}
	className := controllerID
	if resolved, ok := container.ResolveServiceId(controllerID); ok {
		className = resolved
	}
	className = normalizeFQN(className)
	if className == "" {
		return nil, "", false
	}
	path, _, found := php.Resolve(store, className)
	if !found {
		return nil, "", false
	}
	doc, err := store.Get(path)
	if err != nil {
		return nil, "", false
	}
	return doc, utils.PathToURI(path), true
}

func indexedMethodRange(path, className, method string, doc *php.Document, store *php.DocumentStore) (protocol.Range, bool) {
	analysisDoc := doc
	if analysisDoc == nil && store != nil {
		if loaded, err := store.Get(path); err == nil {
			analysisDoc = loaded
		}
	}
	if analysisDoc == nil {
		return protocol.Range{}, false
	}

	index := analysisDoc.Index()
	if len(index.PublicFunctions) == 0 {
		return protocol.Range{}, false
	}

	prefix := className + "::"
	target := prefix + method

	for _, fn := range index.PublicFunctions {
		if fn.Name == target {
			if rng, ok := lineColumnRangeToProtocol(fn.Range); ok {
				return rng, true
			}
		}
	}

	for _, fn := range index.PublicFunctions {
		if strings.HasPrefix(fn.Name, prefix) {
			if rng, ok := lineColumnRangeToProtocol(fn.Range); ok {
				return rng, true
			}
		}
	}

	return protocol.Range{}, false
}

func lineColumnRangeToProtocol(r php.LineColumnRange) (protocol.Range, bool) {
	if r.StartLine <= 0 && r.EndLine <= 0 {
		return protocol.Range{}, false
	}
	startLine := max(r.StartLine-1, 0)
	endLine := r.EndLine - 1
	if endLine < 0 {
		endLine = startLine
	}
	if r.EndLine == 0 && r.StartLine > 0 {
		endLine = r.StartLine - 1
	}
	startCol := max(r.StartColumn, 0)
	endCol := r.EndColumn
	if endCol < 0 {
		endCol = startCol
	}
	return protocol.Range{
		Start: protocol.Position{Line: uint32(startLine), Character: uint32(startCol)},
		End:   protocol.Position{Line: uint32(endLine), Character: uint32(endCol)},
	}, true
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
