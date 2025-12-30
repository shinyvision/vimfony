package analyzer

import (
	sitter "github.com/alexaandru/go-tree-sitter-bare"
	"github.com/shinyvision/vimfony/internal/config"
	php "github.com/shinyvision/vimfony/internal/php"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// Any analyzer may implement this contract. You'll probably create your own
// sub-interface for your own autocompletions.
type Analyzer interface {
	// Gets called by state.SetDocument when our code changes
	Changed(code []byte, change *sitter.InputEdit) error
	// When a document is closed. Wanting to close the tree-sitter tree
	Close()
}

// CompletionProvider is a sub-interface for analyzers that can provide completions
type CompletionProvider interface {
	OnCompletion(pos protocol.Position) ([]protocol.CompletionItem, error)
}

// DefinitionProvider is a sub-interface for analyzers that can provide definitions
type DefinitionProvider interface {
	OnDefinition(pos protocol.Position) ([]protocol.Location, error)
}

// CodeActionProvider is a sub-interface for analyzers that can provide code actions
type CodeActionProvider interface {
	OnCodeAction(context *glsp.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error)
}

// ContainerAware is an interface for analyzers that need access to container configuration
type ContainerAware interface {
	SetContainerConfig(container *config.ContainerConfig)
}

// RoutesAware is an interface for analyzers that need access to routes
type RoutesAware interface {
	SetRoutes(routes *config.RoutesMap)
}

// AutoloadAware is an interface for analyzers that need access to Composer autoload mappings.
type AutoloadAware interface {
	SetAutoloadMap(autoload *config.AutoloadMap)
}

// DocumentStoreAware allows analyzers to receive a shared PHP document store.
type DocumentStoreAware interface {
	SetDocumentStore(store *php.DocumentStore)
}

// DocumentPathAware allows analyzers to be informed of their source file path.
type DocumentPathAware interface {
	SetDocumentPath(path string)
}
