package state

import (
	protocol "github.com/tliron/glsp/protocol_3_16"
	"strings"
)

// Document represents a document in the state.
type Document struct {
	Text       string
	LanguageID string
}

// IsInAutoconfigure checks if the given line number is inside an #[Autoconfigure] block.
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

// HasServicePrefix checks if the cursor is in a position to complete a service.
// If so, it returns the prefix string just after the @, otherwise an empty string.
func (d *Document) HasServicePrefix(pos protocol.Position) (bool, string) {
	lines := strings.Split(d.Text, "\n")
	if pos.Line >= uint32(len(lines)) {
		return false, ""
	}
	line := lines[pos.Line]
	if pos.Character > uint32(len(line)) {
		return false, ""
	}
	lineUntilCursor := line[:pos.Character]

	atIndex := strings.LastIndex(lineUntilCursor, "@")
	if atIndex == -1 {
		return false, ""
	}

	switch d.LanguageID {
	case "php":
		// check if we are inside a string
		sQuoteCountBefore := strings.Count(lineUntilCursor[:atIndex], "'")
		sQuoteCountAfter := strings.Count(line[pos.Character:], "'")
		inSingleQuotes := sQuoteCountBefore%2 == 1 && sQuoteCountAfter%2 == 1

		dQuoteCountBefore := strings.Count(lineUntilCursor[:atIndex], "\"")
		dQuoteCountAfter := strings.Count(line[pos.Character:], "\"")
		inDoubleQuotes := dQuoteCountBefore%2 == 1 && dQuoteCountAfter%2 == 1

		if !inSingleQuotes && !inDoubleQuotes {
			return false, ""
		}
	case "yaml":
		if strings.Contains(lineUntilCursor[:atIndex], "#") {
			return false, ""
		}
	default:
		return false, ""
	}

	return true, lineUntilCursor[atIndex+1:]
}

