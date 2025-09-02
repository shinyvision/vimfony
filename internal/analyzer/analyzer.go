package analyzer

// Any analyzer may implement this contract. You'll probably create your own
// sub-interface for your own autocompletions.
type Analyzer interface {
	// Gets called by state.SetDocument when our code changes
	Changed(code []byte) error
	// When a document is closed. Wanting to close the tree-sitter tree
	Close()
}
