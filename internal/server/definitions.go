package server

import (
	"github.com/shinyvision/vimfony/internal/php"
	"github.com/shinyvision/vimfony/internal/state"
	"github.com/shinyvision/vimfony/internal/twig"
	"github.com/shinyvision/vimfony/internal/utils"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func (s *Server) onDefinition(_ *glsp.Context, params *protocol.DefinitionParams) (any, error) {
	doc, ok := s.state.GetDocument(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}

	if loc, ok := s.resolveTwigPath(doc, params.Position); ok {
		return loc, nil
	}

	if loc, ok := s.resolveTwigFunction(doc, params.Position); ok {
		return loc, nil
	}

	if loc, ok := s.resolvePhpClass(doc, params.Position); ok {
		return loc, nil
	}

	if loc, ok := s.resolveServiceId(doc, params.Position); ok {
		return loc, nil
	}

	return nil, nil
}

func (s *Server) resolveTwigPath(doc *state.Document, position protocol.Position) ([]protocol.Location, bool) {
	if twigPath, ok := twig.PathAt(doc.Text, position); ok {
		if target, ok := twig.Resolve(twigPath, s.config.Container); ok {
			loc := protocol.Location{
				URI:   protocol.DocumentUri(utils.PathToURI(target)),
				Range: protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 0}},
			}
			return []protocol.Location{loc}, true
		}
	}
	return nil, false
}

func (s *Server) resolveTwigFunction(doc *state.Document, position protocol.Position) ([]protocol.Location, bool) {
	if doc.LanguageID == "twig" {
		if twigFunc, ok := twig.FunctionAt(doc.Text, position); ok {
			if target, functionRange, ok := twig.ResolveFunction(twigFunc, s.config); ok {
				loc := protocol.Location{
					URI:   protocol.DocumentUri(utils.PathToURI(target)),
					Range: functionRange,
				}
				return []protocol.Location{loc}, true
			}
		}
	}
	return nil, false
}

func (s *Server) resolvePhpClass(doc *state.Document, position protocol.Position) ([]protocol.Location, bool) {
	if phpClassName, ok := php.PathAt(doc.Text, position); ok {
		if target, classRange, ok := php.Resolve(phpClassName, s.config.Psr4, s.config.Container.WorkspaceRoot); ok {
			loc := protocol.Location{
				URI:   protocol.DocumentUri(utils.PathToURI(target)),
				Range: classRange,
			}
			return []protocol.Location{loc}, true
		}
	}
	return nil, false
}

func (s *Server) resolveServiceId(doc *state.Document, position protocol.Position) ([]protocol.Location, bool) {
	findClass := func(serviceID string) ([]protocol.Location, bool) {
		if className, ok := s.config.Container.ResolveServiceId(serviceID); ok {
			if target, classRange, ok := php.Resolve(className, s.config.Psr4, s.config.Container.WorkspaceRoot); ok {
				loc := protocol.Location{
					URI:   protocol.DocumentUri(utils.PathToURI(target)),
					Range: classRange,
				}
				return []protocol.Location{loc}, true
			}
		}
		return nil, false
	}
	line, ok := doc.GetLine(int(position.Line))
	size := len(line)
	if !ok {
		return nil, false
	}
	isServiceChar := func(b byte) bool {
		return (b >= 'a' && b <= 'z') ||
			(b >= 'A' && b <= 'Z') ||
			(b >= '0' && b <= '9') ||
			b == '_' || b == '.' || b == '-' || b == '\\'
	}
	left := 0
	right := 0
	offset := int(position.Character)
	for {
		if offset-left == 0 || !isServiceChar(line[offset-left-1]) {
			break
		}
		left++
	}
	for {
		if offset+right == size || !isServiceChar(line[offset+right]) {
			break
		}
		right++
	}
	serviceID := line[offset-left : offset+right]
	return findClass(serviceID)
}
