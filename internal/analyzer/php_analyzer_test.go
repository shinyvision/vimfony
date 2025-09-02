package analyzer

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestIsInAutoconfigure(t *testing.T) {
	content, err := os.ReadFile("../../mock/class_with_autoconfigure.php")
	require.NoError(t, err)

	analyzer := NewPHPAnalyzer()
	analyzer.Changed(content, nil)

	// Test case 1: Inside autoconfigure
	pos1 := protocol.Position{Line: 11, Character: 23}
	found1, msg1 := analyzer.(PhpAnalyzer).IsInAutoconfigure(pos1)
	require.True(t, found1, "Test case 1 failed: %s", msg1)

	// Test case 2: Outside autoconfigure
	pos2 := protocol.Position{Line: 20, Character: 14}
	found2, _ := analyzer.(PhpAnalyzer).IsInAutoconfigure(pos2)
	require.False(t, found2)
}

func BenchmarkIsInAutoconfigure(b *testing.B) {
	content, err := os.ReadFile("../../mock/class_with_autoconfigure.php")
	if err != nil {
		b.Fatalf("failed to read mock PHP file: %v", err)
	}

	analyzer := NewPHPAnalyzer()
	analyzer.Changed(content, nil)
	pos := protocol.Position{Line: 11, Character: 23}

	b.ReportAllocs()

	for b.Loop() {
		_, _ = analyzer.(PhpAnalyzer).IsInAutoconfigure(pos)
	}
}
