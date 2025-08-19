package twig

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/shinyvision/vimfony/internal/config"
	"github.com/tliron/commonlog"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

var twigReQuoted = regexp.MustCompile(`["']([^'"\\]*(?:\\.[^'"\\]*)*\.twig)["']`)
var twigReBare = regexp.MustCompile(`([@A-Za-z0-9_./:-]+\.twig)`)

// PathAt returns the Twig path at a given position in the content.
func PathAt(content string, pos protocol.Position) (string, bool) {
	offset := pos.IndexIn(content) // LSP UTF-16 -> byte offset

	// helper: search with a regex whose capture group 1 is the path
	findWith := func(re *regexp.Regexp) (string, bool) {
		idxs := re.FindAllStringSubmatchIndex(content, -1)
		for _, m := range idxs {
			// m[0], m[1] = full match; m[2], m[3] = group 1 (the path)
			if len(m) >= 4 && m[0] <= offset && offset <= m[1] {
				start, end := m[2], m[3]
				if 0 <= start && start <= end && end <= len(content) {
					return content[start:end], true
				}
			}
		}
		return "", false
	}

	// Prefer quoted first, then bare
	if p, ok := findWith(twigReQuoted); ok {
		return p, true
	}
	if p, ok := findWith(twigReBare); ok {
		return p, true
	}
	return "", false
}

func normalize(p string) string {
	// Symfony-ish variants: "@Bundle/path.twig" or "bundle:section/file.twig"
	p = strings.TrimPrefix(p, "@")
	p = strings.ReplaceAll(p, ":", "/")
	p = strings.TrimPrefix(p, "/")
	return filepath.FromSlash(p)
}

// Resolve resolves a Twig path to an absolute file path.
func Resolve(rel string, cfg *config.ContainerConfig) (string, bool) {
	orig := rel
	rel = normalize(rel)

	candidatesTried := make([]string, 0, 8)

	// Try bundle resolution first: "<Bundle>/path/to/file.twig"
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	if len(parts) == 2 {
		bundle, remainder := parts[0], parts[1]
		if bases, ok := cfg.BundleRoots[bundle]; ok {
			for _, base := range bases {
				cand := filepath.Join(base, remainder)
				candidatesTried = append(candidatesTried, cand)
				if info, err := os.Stat(cand); err == nil && !info.IsDir() {
					return cand, true
				}
			}
		}
	}

	// Fall back to bare roots
	for _, root := range cfg.Roots {
		var base string
		if filepath.IsAbs(root) {
			base = root
		} else {
			base = filepath.Join(cfg.WorkspaceRoot, root)
		}
		cand := filepath.Join(base, rel)
		candidatesTried = append(candidatesTried, cand)
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand, true
		}
	}

	// Log failure details
	logger := commonlog.GetLoggerf("vimfony.twig")
	if len(candidatesTried) == 0 {
		logger.Infof("definition not found for twig path '%s' (normalized '%s'): no candidates tried", orig, rel)
	} else {
		logger.Infof("definition not found for twig path '%s' (normalized '%s'); tried %d candidates, last: %s",
			orig, rel, len(candidatesTried), candidatesTried[len(candidatesTried)-1],
		)
		for i, c := range candidatesTried {
			logger.Debugf("candidate %d: %s", i+1, c)
		}
	}

	return "", false
}
