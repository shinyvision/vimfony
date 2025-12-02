package translations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseIntlIcu(t *testing.T) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "vimfony_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a messages+intl-icu.en.yaml file
	content := `
sylius.ui.item.choice: "{count, plural, =0 {} one {, 1 item} other {, # items}}"
simple.key: "Simple Value"
`
	filename := filepath.Join(tmpDir, "messages+intl-icu.en.yaml")
	err = os.WriteFile(filename, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Parse it
	translations := Parse([]string{filename})

	// Check if keys exist
	if _, ok := translations["sylius.ui.item.choice"]; !ok {
		t.Errorf("Expected key 'sylius.ui.item.choice' to be found")
	}
	if _, ok := translations["simple.key"]; !ok {
		t.Errorf("Expected key 'simple.key' to be found")
	}

	// Test multiline
	contentMultiline := `
multiline.key: |
  {count, plural,
  =1 {One}
  other {Many}
  }
`
	filenameMultiline := filepath.Join(tmpDir, "multiline.en.yaml")
	err = os.WriteFile(filenameMultiline, []byte(contentMultiline), 0644)
	if err != nil {
		t.Fatal(err)
	}

	translationsMultiline := Parse([]string{filenameMultiline})
	if _, ok := translationsMultiline["multiline.key"]; !ok {
		t.Errorf("Expected key 'multiline.key' to be found")
	}

	// Test value on next line
	contentNextLine := `
nextline.key:
  "{count, plural, =0 {None} other {Many}}"
`
	filenameNextLine := filepath.Join(tmpDir, "nextline.en.yaml")
	err = os.WriteFile(filenameNextLine, []byte(contentNextLine), 0644)
	if err != nil {
		t.Fatal(err)
	}

	translationsNextLine := Parse([]string{filenameNextLine})
	if _, ok := translationsNextLine["nextline.key"]; !ok {
		t.Errorf("Expected key 'nextline.key' to be found")
	}
}
