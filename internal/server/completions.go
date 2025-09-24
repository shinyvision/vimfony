package server

import (
	"github.com/shinyvision/vimfony/internal/analyzer"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func (s *Server) onCompletion(_ *glsp.Context, p *protocol.CompletionParams) (any, error) {
	doc, ok := s.state.GetDocument(p.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	if doc.Analyzer != nil {
		if cp, ok := doc.Analyzer.(analyzer.CompletionProvider); ok {
			completions, err := cp.OnCompletion(p.Position)
			if err != nil {
				return nil, err
			}
			if len(completions) > 0 {
				return completions, nil
			}
		}
	}

	return nil, nil
}
