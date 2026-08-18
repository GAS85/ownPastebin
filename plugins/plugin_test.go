package plugins

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

type testPlugin struct {
	css   []string
	js    []string
	init  string
	embed fs.FS
}

func (p *testPlugin) CSSImports(prefix string) []string { return p.css }
func (p *testPlugin) JSImports(prefix string) []string  { return p.js }
func (p *testPlugin) JSInit() string                    { return p.init }
func (p *testPlugin) StaticFS() fs.FS                   { return p.embed }

func TestDefaultBaseAssets(t *testing.T) {
	base := DefaultBase("/prefix")
	if len(base.CSSImports) != 2 {
		t.Fatalf("expected 2 CSS imports, got %d", len(base.CSSImports))
	}
	if got := base.CSSImports[0]; got != "/prefix/static/w3.css" {
		t.Fatalf("unexpected first CSS import: %q", got)
	}
	if len(base.JSImports) != 1 {
		t.Fatalf("expected 1 JS import, got %d", len(base.JSImports))
	}
	if got := base.JSImports[0]; got != "/prefix/static/custom.js" {
		t.Fatalf("unexpected second JS import: %q", got)
	}
}

func TestDedupeStringsPreservesOrder(t *testing.T) {
	input := []string{"a", "b", "a", "c", "b"}
	want := []string{"a", "b", "c"}
	if got := dedupeStrings(input); len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	} else {
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("expected element %d to be %q, got %q", i, want[i], got[i])
			}
		}
	}
}

func TestManagerBuildForFiltersConditionalPlugins(t *testing.T) {
	staticFS := fstest.MapFS{"asset.txt": {Data: []byte("ok")}}
	base := DefaultBase("/base")
	manager := NewManager(base, []Plugin{
		&PrismPlugin{EmbeddedFS: staticFS},
		&MermaidPlugin{},
		&testPlugin{css: []string{"/base/static/test.css"}, js: []string{"/base/static/test.js"}, init: "initTest", embed: nil},
	})

	if len(manager.StaticFileSystems) != 1 {
		t.Fatalf("expected 1 static filesystem, got %d", len(manager.StaticFileSystems))
	}
	if got := manager.TailCSSImports(); len(got) != 1 || got[0] != "/base/static/custom.css" {
		t.Fatalf("unexpected tail CSS imports: %v", got)
	}

	css, js, inits := manager.BuildFor("mermaid")
	if len(css) == 0 || len(js) == 0 {
		t.Fatal("expected assets for mermaid language")
	}
	if got := css[len(css)-1]; got != "/base/static/test.css" {
		t.Fatalf("unexpected CSS asset order or content: %q", got)
	}
	if got := js[len(js)-1]; got != "/base/static/test.js" {
		t.Fatalf("unexpected JS asset order or content: %q", got)
	}
	if len(inits) != 3 {
		t.Fatalf("expected 3 init snippets, got %d", len(inits))
	}
}

func TestManagerBuildForExcludesInactiveConditionalPlugins(t *testing.T) {
	base := DefaultBase("/x")
	manager := NewManager(base, []Plugin{&MermaidPlugin{}})

	cssDefault, _, initsDefault := manager.BuildFor("")
	if len(cssDefault) != 2 {
		t.Fatalf("expected only base CSS imports for no language, got %d", len(cssDefault))
	}
	if len(initsDefault) != 0 {
		t.Fatal("expected no init snippets for empty language")
	}
}

func TestPrismPluginStaticFS(t *testing.T) {
	embed := fstest.MapFS{"prism.js": {Data: []byte("x")}}
	plugin := &PrismPlugin{EmbeddedFS: embed}
	if got := plugin.StaticFS(); got == nil {
		t.Fatal("expected embedded FS to be returned")
	}
}
