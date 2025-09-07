package state

import (
	"regexp"
	"strings"

	"github.com/shinyvision/vimfony/internal/analyzer"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type Document struct {
	Text       string
	LanguageID string
	lines      []string
	Analyzer   analyzer.Analyzer
}

func NewDocument(languageId string, content string) *Document {
	doc := &Document{
		LanguageID: languageId,
		Text:       content,
		lines:      strings.Split(content, "\n"),
	}
	switch languageId {
	case "php":
		doc.Analyzer = analyzer.NewPHPAnalyzer()
	case "xml":
		doc.Analyzer = analyzer.NewXMLAnalyzer()
	case "twig":
		doc.Analyzer = analyzer.NewTwigAnalyzer()
	}
	return doc
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
