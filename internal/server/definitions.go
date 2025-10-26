package server

import (
	"github.com/shinyvision/vimfony/internal/analyzer"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func (s *Server) onDefinition(_ *glsp.Context, params *protocol.DefinitionParams) (any, error) {
	doc, ok := s.state.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	if doc.Analyzer != nil {
		if provider, ok := doc.Analyzer.(analyzer.DefinitionProvider); ok {
			locations, err := provider.OnDefinition(params.Position)
			if err != nil {
				return nil, err
			}
			if len(locations) > 0 {
				return locations, nil
			}
		}
	}

	return nil, nil
}
