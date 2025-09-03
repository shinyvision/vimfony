package analyzer

import (
	"bytes"
	"unicode/utf8"

	sitter "github.com/smacker/go-tree-sitter"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Shortens a PHP FQN to its base class name
func shortName(qualified string) string {
	if i := lastIndexByte(qualified, '\\'); i >= 0 && i+1 < len(qualified) {
		return qualified[i+1:]
	}
	return qualified
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// Checks if a tree-sitter point is within a node... usually our vim caret
func caretIn(n *sitter.Node, pt sitter.Point) bool {
	if n == nil {
		return false
	}
	sp, ep := n.StartPoint(), n.EndPoint()
	// So columns can be at different positions, so it's easier to check for rows
	return sp.Row <= pt.Row && pt.Row <= ep.Row
}

// UTF-16 conversion from bytes to point
func lspPosToPoint(pos protocol.Position, content []byte) (sitter.Point, bool) {
	row := uint32(pos.Line)

	var lineStart, i, curRow uint32
	for i = 0; i < uint32(len(content)) && curRow < row; i++ {
		if content[i] == '\n' {
			curRow++
			lineStart = i + 1
		}
	}
	if curRow != row {
		return sitter.Point{}, false
	}

	need := pos.Character
	var colBytes uint32
	for offset := lineStart; offset < uint32(len(content)); {
		b := content[offset]
		if b == '\n' || b == '\r' {
			break
		}
		rune, size := utf8.DecodeRune(content[offset:])
		var u16len uint32 = 1
		if rune > 0xFFFF {
			u16len = 2
		}
		if need < u16len {
			colBytes = offset - lineStart
			return sitter.Point{Row: row, Column: colBytes}, true
		}
		need -= u16len
		offset += uint32(size)
		colBytes = offset - lineStart
	}
	return sitter.Point{Row: row, Column: colBytes}, true
}

// Getting our line until the caret
func linePrefixAtPoint(content []byte, point sitter.Point) []byte {
	start := 0
	rows := int(point.Row)
	for i := 0; i < len(content) && rows > 0; i++ {
		if content[i] == '\n' {
			start = i + 1
			rows--
		}
	}
	caret := min(start+int(point.Column), len(content))
	return content[start:caret]
}

func lspPosToByteOffset(content []byte, pos protocol.Position) int {
	lines := bytes.Split(content, []byte("\n"))
	if int(pos.Line) >= len(lines) {
		return -1
	}
	offset := 0
	for i := 0; i < int(pos.Line); i++ {
		offset += len(lines[i]) + 1 // +1 for the newline
	}

	char := int(pos.Character)
	lineLen := len(lines[int(pos.Line)])
	if char > lineLen {
		char = lineLen
	}

	offset += char

	if offset > len(content) {
		return -1
	}
	return offset
}
