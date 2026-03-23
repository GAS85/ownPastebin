package plugins

import (
	"io/fs"
)

// PrismPlugin adds Prism.js syntax highlighting.
// Static files (prism.js, prism.css) are expected to be embedded by the
// caller via go:embed in main.go and passed in via EmbeddedFS.
type PrismPlugin struct {
	EmbeddedFS fs.FS // embed.FS sub-tree containing static/prism.*
}

func (p *PrismPlugin) CSSImports() []string { return []string{"/static/prism.css"} }
func (p *PrismPlugin) JSImports() []string  { return []string{"/static/prism.js"} }
func (p *PrismPlugin) JSInit() string {
	return "var holder = document.getElementById('pastebin-code-block'); " +
		"if (holder) { Prism.highlightElement(holder); }"
}
func (p *PrismPlugin) StaticFS() fs.FS { return p.EmbeddedFS }
