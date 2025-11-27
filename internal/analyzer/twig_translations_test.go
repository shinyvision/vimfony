package analyzer

import (
	"testing"

	"github.com/shinyvision/vimfony/internal/config"
	"github.com/shinyvision/vimfony/internal/translations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TestTwigTranslationCompletion(t *testing.T) {
	content := `{{ 'hello'|trans }}
{{ 'messages.'|trans }}
{{ 'foo'|t }}
`
	an := NewTwigAnalyzer().(*twigAnalyzer)

	container := &config.ContainerConfig{
		TranslationKeys: map[string][]translations.TranslationLocation{
			"hello.world":      {{URI: "file:///tmp/messages.en.yaml"}},
			"messages.welcome": {{URI: "file:///tmp/messages.en.yaml"}},
			"foo.bar":          {{URI: "file:///tmp/messages.en.yaml"}},
		},
	}
	an.SetContainerConfig(container)
	require.NoError(t, an.Changed([]byte(content), nil))

	// Test 1: Complete 'hello'
	pos := protocol.Position{Line: 0, Character: 8} // 'hello|'
	items, err := an.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	found := false
	for _, item := range items {
		if item.Label == "hello.world" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected hello.world completion")

	// Test 2: Complete 'messages.'
	pos = protocol.Position{Line: 1, Character: 12} // 'messages.|'
	items, err = an.OnCompletion(pos)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	found = false
	for _, item := range items {
		if item.Label == "messages.welcome" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected messages.welcome completion")

	// Test 3: Complete 'foo' with |t filter
	pos = protocol.Position{Line: 2, Character: 6} // 'foo|'
	items, err = an.OnCompletion(pos)
	require.NoError(t, err)

	found = false
	for _, item := range items {
		if item.Label == "foo.bar" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected foo.bar completion with |t filter")
}

func TestTwigTranslationDefinition(t *testing.T) {
	content := `{{ 'hello.world'|trans }}`
	an := NewTwigAnalyzer().(*twigAnalyzer)

	expectedURI := "file:///tmp/messages.en.yaml"
	container := &config.ContainerConfig{
		TranslationKeys: map[string][]translations.TranslationLocation{
			"hello.world": {{
				URI: expectedURI,
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 0, Character: 10},
				},
			}},
		},
	}
	an.SetContainerConfig(container)
	require.NoError(t, an.Changed([]byte(content), nil))

	pos := protocol.Position{Line: 0, Character: 5} // inside 'hello.world'
	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.Len(t, locs, 1)
	assert.Equal(t, expectedURI, string(locs[0].URI))
}

func TestTwigTranslationDefinitionMultipleLocations(t *testing.T) {
	content := `{{ 'hello.world'|trans }}`
	an := NewTwigAnalyzer().(*twigAnalyzer)

	locEn := translations.TranslationLocation{URI: "file:///tmp/messages.en.yaml"}
	locFr := translations.TranslationLocation{URI: "file:///tmp/messages.fr.yaml"}

	container := &config.ContainerConfig{
		TranslationKeys: map[string][]translations.TranslationLocation{
			"hello.world": {locEn, locFr},
		},
		DefaultLocale: "en",
	}
	an.SetContainerConfig(container)
	require.NoError(t, an.Changed([]byte(content), nil))

	// With DefaultLocale = "en", should return only en location
	pos := protocol.Position{Line: 0, Character: 5}
	locs, err := an.OnDefinition(pos)
	require.NoError(t, err)
	require.Len(t, locs, 1)
	assert.Equal(t, "file:///tmp/messages.en.yaml", string(locs[0].URI))

	// Without DefaultLocale, should return both
	container.DefaultLocale = ""
	locs, err = an.OnDefinition(pos)
	require.NoError(t, err)
	require.Len(t, locs, 2)
}
