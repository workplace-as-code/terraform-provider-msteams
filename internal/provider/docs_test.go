package provider

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The documentation is published on the Terraform Registry, which serves the
// overview at .../docs without a trailing slash and does not resolve .md
// links: a relative link breaks there or on GitHub. Pages link each other with
// absolute registry URLs, checked here against the generated files.
const registryDocs = "https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs"

var markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

func TestDocsLinks(t *testing.T) {
	pages, err := filepath.Glob("../../docs/*.md")
	if err != nil {
		t.Fatal(err)
	}
	more, err := filepath.Glob("../../docs/*/*.md")
	if err != nil {
		t.Fatal(err)
	}
	pages = append(pages, more...)
	if len(pages) == 0 {
		t.Fatal("no documentation page found")
	}
	for _, page := range pages {
		data, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimPrefix(page, "../../")
		for _, m := range markdownLink.FindAllStringSubmatch(string(data), -1) {
			link := m[1]
			switch {
			case strings.HasPrefix(link, registryDocs):
				target, _, _ := strings.Cut(strings.TrimPrefix(link, registryDocs), "#")
				file := "index"
				if target != "" {
					file = strings.TrimPrefix(target, "/")
				}
				if _, err := os.Stat("../../docs/" + file + ".md"); err != nil {
					t.Errorf("%s: %s points to a page that does not exist (docs/%s.md)", name, link, file)
				}
			case strings.HasPrefix(link, "http://"), strings.HasPrefix(link, "https://"),
				strings.HasPrefix(link, "mailto:"), strings.HasPrefix(link, "#"):
			default:
				t.Errorf("%s: relative link %q: use %s/...", name, link, registryDocs)
			}
		}
	}
}
