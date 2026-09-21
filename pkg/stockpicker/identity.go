package stockpicker

import (
	"fmt"
	"strings"

	"github.com/raghavkgarg/mycase/pkg/csvloader"
)

// SanitizeName normalizes a display/universe name into the token used to build
// report directories, CSV filenames, and PIT snapshot filenames. It is the single
// source of truth for that transformation so writers and readers compose the same
// path. Lowercases, and maps the separators that would otherwise fragment a name
// (comma, plus, whitespace) to underscore while stripping the caret used in raw
// benchmark symbols (e.g. "^GSPC").
func SanitizeName(name string) string {
	r := strings.NewReplacer(
		",", "_",
		"+", "_",
		" ", "_",
		"^", "",
	)
	return strings.ToLower(r.Replace(strings.TrimSpace(name)))
}

// DisplayName resolves the human-facing universe label for a run, honoring the
// flags the user actually passed. Precedence:
//
//  1. opts.DisplayName (explicit --name) always wins.
//  2. opts.IndexName (--index) anchors the identity when set, so a run is filed
//     under the index the user asked for — not a name derived from a --file CSV's
//     filename (the source of the historical us_microsmall_multibagger bug).
//  3. Otherwise fall back to the loaded source name (loadedName), i.e. the
//     GetUniverseName-derived label for a pure --file / custom-tickers run.
//
// It does not sanitize; callers that need a path token wrap the result in
// SanitizeName. This keeps the printed "Index/File:" label readable while paths
// stay normalized.
func DisplayName(opts *Options, loadedName string) string {
	if opts.DisplayName != "" {
		return opts.DisplayName
	}
	if opts.IndexName != "" {
		return opts.IndexName
	}
	return loadedName
}

// PickIdentity returns the "<name>_<method>" token that identifies a run's
// artifacts (report dir, index-picks CSV, PIT snapshot). Both the writer
// (RunWithResult) and the cached-run reader (cmd) must build paths from this so
// they always agree.
func PickIdentity(displayName, method string) string {
	return fmt.Sprintf("%s_%s", SanitizeName(displayName), method)
}

// GetUniverseName re-exports csvloader.GetUniverseName so the command layer can
// resolve a --file universe label without importing csvloader directly. Kept thin
// intentionally.
func GetUniverseName(filePath string) string {
	return csvloader.GetUniverseName(filePath)
}
