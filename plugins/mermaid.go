package plugins

import "io/fs"

// MermaidPlugin adds Mermaid diagram rendering.
type MermaidPlugin struct{}

func (p *MermaidPlugin) CSSImports() []string { return nil }
func (p *MermaidPlugin) JSImports() []string {
	return []string{
		"/static/mermaid.min.js",
	}
}
func (p *MermaidPlugin) JSInit() string {
	return "mermaid.initialize({ startOnLoad: false }); " +
		"mermaid.run({ querySelector: '.language-mermaid' });"
}
func (p *MermaidPlugin) StaticFS() fs.FS { return nil }
