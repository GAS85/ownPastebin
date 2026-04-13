package plugins

import "io/fs"

// MermaidPlugin provides mermaid.min.js for diagram rendering.
//
// It implements ConditionalPlugin so its assets are included only when the
// paste language is "mermaid". On every other page (including the new-paste
// editor) mermaid.min.js is never loaded.
//
// When the user switches the language selector to "mermaid" in the browser,
// custom.js lazy-loads mermaid.min.js on demand so diagrams still render
// without a page reload.
type MermaidPlugin struct{}

// ActiveFor implements ConditionalPlugin.
func (p *MermaidPlugin) ActiveFor(lang string) bool {
	return lang == "mermaid"
}

func (p *MermaidPlugin) CSSImports(_ string) []string {
	return nil // mermaid has no separate stylesheet
}

func (p *MermaidPlugin) JSImports(prefix string) []string {
	return []string{
		prefix + "/static/mermaid.min.js",
	}
}

func (p *MermaidPlugin) JSInit() string {
	return "initMermaid"
}

func (p *MermaidPlugin) StaticFS() fs.FS {
	return nil
}
