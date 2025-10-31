package php

import (
	"os"
	"regexp"
	"strings"

	"github.com/shinyvision/vimfony/internal/config"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

var classNameRe = regexp.MustCompile(`([A-Z][a-zA-Z0-9_]*\\)+[a-zA-Z0-9_]+`)
var classDefinitionRe = regexp.MustCompile(`(class|trait|interface)\s+([a-zA-Z0-9_]+)`) // Capture group 2 is the class name

func PathAt(content string, pos protocol.Position) (string, bool) {
	offset := pos.IndexIn(content)

	idxs := classNameRe.FindAllStringSubmatchIndex(content, -1)
	for _, m := range idxs {
		if len(m) >= 2 && m[0] <= offset && offset <= m[1] {
			start, end := m[0], m[1]
			if 0 <= start && start <= end && end <= len(content) {
				return content[start:end], true
			}
		}
	}

	return "", false
}

func Resolve(className string, autoloadMap config.AutoloadMap, workspaceRoot string) (string, protocol.Range, bool) {
	path, ok := config.AutoloadResolve(className, autoloadMap, workspaceRoot)
	if !ok {

		return "", protocol.Range{}, false
	}

	// Found the file, now find the class definition within it
	content, err := os.ReadFile(path)
	if err != nil {
		return "", protocol.Range{}, false
	}

	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		match := classDefinitionRe.FindStringSubmatchIndex(line)
		if len(match) >= 4 {
			// match[4] is start of class name, match[5] is end of class name
			classRange := protocol.Range{
				Start: protocol.Position{Line: uint32(i), Character: uint32(match[4])},
				End:   protocol.Position{Line: uint32(i), Character: uint32(match[5])},
			}
			return path, classRange, true
		}
	}
	return path, protocol.Range{}, true // Found file, but not class definition
}

func FindMethodRange(path, methodName string) (protocol.Range, bool) {
	content, err := os.ReadFile(path)
	if err != nil {
		return protocol.Range{}, false
	}

	methodPattern := regexp.MustCompile(`(?i)function\s+(` + regexp.QuoteMeta(methodName) + `)\s*\(`)
	loc := methodPattern.FindStringSubmatchIndex(string(content))
	if loc == nil || len(loc) < 4 {
		return protocol.Range{}, false
	}

	start := loc[2]
	end := loc[3]

	text := string(content)
	line := strings.Count(text[:start], "\n")
	lastNewline := strings.LastIndex(text[:start], "\n")
	startCol := start
	endCol := end
	if lastNewline >= 0 {
		startCol = start - lastNewline - 1
		endCol = end - lastNewline - 1
	}

	return protocol.Range{
		Start: protocol.Position{Line: uint32(line), Character: uint32(startCol)},
		End:   protocol.Position{Line: uint32(line), Character: uint32(endCol)},
	}, true
}
