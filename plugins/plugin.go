package plugins

import "io/fs"

// Plugin contributes CSS, JS, init snippets, and embedded static files.
type Plugin interface {
	CSSImports() []string
	JSImports() []string
	JSInit() string // empty string = no init snippet
	StaticFS() fs.FS // nil = no embedded files
}

// Manager merges all registered plugins with base assets.
type Manager struct {
	CSSImports      []string
	JSImports       []string
	JSInits         []string
	StaticFileSystems []fs.FS
}

func NewManager(base *Base, plugins []Plugin) *Manager {
	m := &Manager{}

	// Base assets first, then plugins extend them
	m.CSSImports = append(m.CSSImports, base.CSSImports...)
	m.JSImports = append(m.JSImports, base.JSImports...)

	for _, p := range plugins {
		m.CSSImports = append(m.CSSImports, p.CSSImports()...)
		m.JSImports = append(m.JSImports, p.JSImports()...)
		if s := p.JSInit(); s != "" {
			m.JSInits = append(m.JSInits, s)
		}
		if fsys := p.StaticFS(); fsys != nil {
			m.StaticFileSystems = append(m.StaticFileSystems, fsys)
		}
	}

	return m
}

// Base holds the core CDN assets shared by all deployments.
type Base struct {
	CSSImports []string
	JSImports  []string
}

func DefaultBase() *Base {
	return &Base{
		CSSImports: []string{
			"https://cdnjs.cloudflare.com/ajax/libs/twitter-bootstrap/4.6.2/css/bootstrap.min.css",
			"https://cdnjs.cloudflare.com/ajax/libs/font-awesome/5.15.4/css/all.min.css",
			"/static/custom.css",
		},
		JSImports: []string{
			"https://cdnjs.cloudflare.com/ajax/libs/jquery/3.7.0/jquery.min.js",
			"https://cdnjs.cloudflare.com/ajax/libs/crypto-js/4.2.0/crypto-js.min.js",
			"https://cdnjs.cloudflare.com/ajax/libs/popper.js/1.16.1/umd/popper.min.js",
			"https://cdnjs.cloudflare.com/ajax/libs/twitter-bootstrap/4.6.2/js/bootstrap.min.js",
			"https://cdnjs.cloudflare.com/ajax/libs/clipboard.js/2.0.11/clipboard.min.js",
			"/static/custom.js",
		},
	}
}
