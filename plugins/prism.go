package plugins

import "io/fs"

// PrismPlugin provides Prism.js syntax-highlighting assets.
//
// It registers only Prism's own CSS and JS — NOT custom.css.
// custom.css is emitted last by Manager.TailCSSImports() so it always wins
// the cascade regardless of what plugins add.
type PrismPlugin struct {
	EmbeddedFS fs.FS
}

func (p *PrismPlugin) CSSImports(prefix string) []string {
	return []string{
		prefix + "/static/prism.css",
	}
}

func (p *PrismPlugin) JSImports(prefix string) []string {
	return []string{
		prefix + "/static/prism.js",
	}
}

func (p *PrismPlugin) JSInit() string {
	return "init_plugins"
}

func (p *PrismPlugin) StaticFS() fs.FS {
	return p.EmbeddedFS
}
