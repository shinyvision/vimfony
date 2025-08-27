package state

import (
	"sync"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// State manages the document state for the language server.
type State struct {
	mu   sync.RWMutex
	docs map[protocol.DocumentUri]*Document
}

func NewState() *State {
	return &State{
		docs: make(map[protocol.DocumentUri]*Document),
	}
}

// GetDocument retrieves a document from the state.
func (s *State) GetDocument(uri protocol.DocumentUri) (*Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	doc, ok := s.docs[uri]
	return doc, ok
}

// SetDocument adds or updates a document in the state.
func (s *State) SetDocument(uri protocol.DocumentUri, text string, languageID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs[uri] = NewDocument(languageID, text)
}

// DeleteDocument removes a document from the state.
func (s *State) DeleteDocument(uri protocol.DocumentUri) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.docs, uri)
}

