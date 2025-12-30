package server

import (
	"github.com/shinyvision/vimfony/internal/analyzer"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func (s *Server) onCodeAction(context *glsp.Context, params *protocol.CodeActionParams) (any, error) {
	doc, ok := s.state.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	if doc.Analyzer != nil {
		if provider, ok := doc.Analyzer.(analyzer.CodeActionProvider); ok {
			codeActions, err := provider.OnCodeAction(context, params)
			if err != nil {
				return nil, err
			}
			if len(codeActions) > 0 {
				return codeActions, nil
			}
		}
	}

	return nil, nil
}
