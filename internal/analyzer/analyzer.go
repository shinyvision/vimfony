package analyzer

import (
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/doctrine"
	php "github.com/shinyvision/vimfony/internal/php"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

type Analyzer interface {
	Changed(code []byte, change *sitter.InputEdit) error
	Close()
}

type CompletionProvider interface {
	OnCompletion(pos protocol.Position) ([]protocol.CompletionItem, error)
}

type DefinitionProvider interface {
	OnDefinition(pos protocol.Position) ([]protocol.Location, error)
}

type CodeActionProvider interface {
	OnCodeAction(context *glsp.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error)
}

type ContainerAware interface {
	SetContainerConfig(container *config.ContainerConfig)
}

type RoutesAware interface {
	SetRoutes(routes *config.RoutesMap)
}

type AutoloadAware interface {
	SetAutoloadMap(autoload *config.AutoloadMap)
}

type DocumentStoreAware interface {
	SetDocumentStore(store *php.DocumentStore)
}

type DocumentPathAware interface {
	SetDocumentPath(path string)
}

type DoctrineAware interface {
	SetDoctrineRegistry(registry *doctrine.Registry)
}
