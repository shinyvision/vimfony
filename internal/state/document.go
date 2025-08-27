package state

import (
	"regexp"
	"strings"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

type Document struct {
	Text       string
	LanguageID string
	lines      []string
}

func NewDocument(languageId string, content string) *Document {
	return &Document{
		LanguageID: languageId,
		Text:       content,
		lines:      strings.Split(content, "\n"),
	}
}

func (d *Document) GetLine(i int) (string, bool) {
	if i < 0 || i >= len(d.lines) {
		return "", false
	}
	return d.lines[i], true
}

func (d *Document) HasServicePrefix(p protocol.Position) (bool, string) {
	line, ok := d.GetLine(int(p.Line))
	if !ok {
		return false, ""
	}

	re := regexp.MustCompile(`services\:\s*([a-zA-Z0-9_.-]*)$`)
	matches := re.FindStringSubmatch(line[:p.Character])
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

func (d *Document) IsInAutoconfigure(lineNumber int) bool {
	if d.LanguageID != "php" {
		return false
	}
	lines := strings.Split(d.Text, "\n")
	if lineNumber >= len(lines) {
		return false
	}

	// Search backwards for autoconfigure attribute
	autoConfigureLineNum := -1
	for i := lineNumber; i >= 0; i-- {
		if strings.Contains(lines[i], "#[Autoconfigure") || strings.Contains(lines[i], "Autoconfigure(") {
			autoConfigureLineNum = i
			break
		}
	}

	if autoConfigureLineNum == -1 {
		return false // Not in an Autoconfigure block
	}

	// Search forwards from the Autoconfigure line to find `class `
	classLineNum := -1
	for i := autoConfigureLineNum; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "class ") {
			classLineNum = i
			break
		}
	}

	// The cursor must be between the start and end of the block.
	if classLineNum != -1 && lineNumber >= classLineNum {
		return false
	}

	return true
}

func (d *Document) IsInXmlServiceTag(pos protocol.Position) (bool, string) {
	if d.LanguageID != "xml" {
		return false, ""
	}
	lines := strings.Split(d.Text, "\n")
	if pos.Line >= uint32(len(lines)) {
		return false, ""
	}
	line := lines[pos.Line]
	if pos.Character > uint32(len(line)) {
		return false, ""
	}
	lineUntilCursor := line[:pos.Character]

	// Are we inside id="..."?
	idAttr := "id=\""
	idStart := strings.LastIndex(lineUntilCursor, idAttr)
	if idStart == -1 {
		return false, ""
	}

	// Check if the attribute is closed after the cursor
	if !strings.Contains(line[pos.Character:], "\"") {
		return false, ""
	}

	// Is this part of an argument tag for a service?
	// Look backwards from the id attribute for the tag opening.
	tagStart := strings.LastIndex(lineUntilCursor[:idStart], "<")
	if tagStart == -1 {
		return false, ""
	}

	// Check the tag itself. We are looking for <argument type="service" ...>
	tagContent := line[tagStart:idStart]
	if strings.Contains(tagContent, "argument") && strings.Contains(tagContent, "type=\"service\"") {
		prefix := lineUntilCursor[idStart+len(idAttr):]
		return true, prefix
	}

	return false, ""
}

func (d *Document) GetServiceIDFromXMLAt(p protocol.Position) (string, bool) {
	line, ok := d.GetLine(int(p.Line))
	if !ok {
		return "", false
	}

	re := regexp.MustCompile(`id="([^"]+)"`)
	matches := re.FindAllStringSubmatchIndex(line, -1)

	for _, match := range matches {
		if len(match) >= 4 {
			start := match[2]
			end := match[3]
			if int(p.Character) >= start && int(p.Character) <= end {
				return line[start:end], true
			}
		}
	}

	return "", false
}

func (d *Document) GetServiceIDFromYAMLAt(p protocol.Position) (string, bool) {
	line, ok := d.GetLine(int(p.Line))
	if !ok {
		return "", false
	}

	reAt := regexp.MustCompile(`@([a-zA-Z0-9_.-]+)`)
	matchesAt := reAt.FindAllStringSubmatchIndex(line, -1)
	for _, match := range matchesAt {
		if len(match) >= 4 {
			start := match[2]
			end := match[3]
			if int(p.Character) >= start && int(p.Character) <= end {
				return line[start:end], true
			}
		}
	}

	return "", false
}
