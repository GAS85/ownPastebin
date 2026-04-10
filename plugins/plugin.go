package plugins

import "io/fs"

// Plugin contributes CSS, JS, init snippets, and embedded static files.
// CSSImports and JSImports receive the deployment path prefix so plugins can
// build absolute asset URLs without knowing the prefix themselves.
type Plugin interface {
	CSSImports(prefix string) []string
	JSImports(prefix string) []string
	JSInit() string // empty string = no init snippet
	StaticFS() fs.FS // nil = no embedded files
}

// ConditionalPlugin is an optional extension of Plugin.
// Plugins that implement it are included by BuildFor only when ActiveFor
// returns true for the current paste language.
// Plugins that do not implement it are treated as always-active.
type ConditionalPlugin interface {
	Plugin
	ActiveFor(lang string) bool
}

// Manager merges all registered plugins with the base assets.
// CSSImports, JSImports, and JSInits are pre-built for the "no language"
// case (new-paste page) and can be used directly when BuildFor is not needed.
type Manager struct {
	PathPrefix        string
	base              *Base
	plugins           []Plugin
	StaticFileSystems []fs.FS

	// Pre-built for the common case (new-paste page, lang = "").
	CSSImports []string
	JSImports  []string
	JSInits    []string
}

// NewManager initialises a Manager with the given base assets and plugins.
// It pre-builds the import lists for the no-language case via BuildFor("").
func NewManager(base *Base, plugins []Plugin) *Manager {
	m := &Manager{
		PathPrefix: base.PathPrefix,
		base:       base,
		plugins:    plugins,
	}

	// Collect embedded static file-systems from all plugins up front.
	for _, p := range plugins {
		if fsys := p.StaticFS(); fsys != nil {
			m.StaticFileSystems = append(m.StaticFileSystems, fsys)
		}
	}

	// Pre-build import lists for the new-paste page (no language active).
	css, js, inits := m.BuildFor("")
	m.CSSImports = css
	m.JSImports = js
	m.JSInits = inits

	return m
}

// BuildFor returns deduplicated CSS/JS/init slices for the given paste
// language, plus a TailCSS slice that must be emitted after all other CSS
// (currently only custom.css so it always wins the cascade).
//
// Plugins implementing ConditionalPlugin are included only when their
// ActiveFor(lang) returns true. All other plugins are always included.
//
// Call this in handleView with the actual paste language, and in
// handleNewPaste with "" to exclude conditional plugins on the editor page.
func (m *Manager) BuildFor(lang string) (cssImports, jsImports, jsInits []string) {
	prefix := m.base.PathPrefix

	// 1. Base CSS — everything except custom.css (which goes in TailCSS).
	cssImports = append(cssImports, m.base.CSSImports...)

	// 2. Base JS.
	jsImports = append(jsImports, m.base.JSImports...)

	// 3. Plugin contributions, filtered by language for conditional plugins.
	for _, p := range m.plugins {
		if cp, ok := p.(ConditionalPlugin); ok && !cp.ActiveFor(lang) {
			continue
		}
		cssImports = append(cssImports, p.CSSImports(prefix)...)
		jsImports = append(jsImports, p.JSImports(prefix)...)
		if s := p.JSInit(); s != "" {
			jsInits = append(jsInits, s)
		}
	}

	cssImports = dedupeStrings(cssImports)
	jsImports = dedupeStrings(jsImports)
	jsInits = dedupeStrings(jsInits)
	return
}

// TailCSSImports returns the stylesheet(s) that must be loaded after all
// other CSS so their rules win the cascade.  Currently this is just
// custom.css.  routes.go emits these via TemplateData.TailCSSImports.
func (m *Manager) TailCSSImports() []string {
	return []string{m.base.PathPrefix + "/static/custom.css"}
}

// dedupeStrings returns a new slice with duplicate strings removed,
// preserving first-occurrence order.
func dedupeStrings(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Base
// ---------------------------------------------------------------------------

// Base holds the core assets shared by all deployments.
// custom.css is intentionally absent from CSSImports — it is returned
// separately by Manager.TailCSSImports() so it is always the last stylesheet
// loaded and its rules always win over plugin-supplied styles.
type Base struct {
	PathPrefix string
	CSSImports []string
	JSImports  []string
}

func DefaultBase(prefix string) *Base {
	static := prefix + "/static"

	return &Base{
		PathPrefix: prefix,
		CSSImports: []string{
			static + "/w3.css",
			static + "/all.min.css",
			// custom.css is NOT here — see Manager.TailCSSImports().
		},
		JSImports: []string{
			static + "/clipboard.min.js",
			static + "/custom.js",
		},
	}
}
