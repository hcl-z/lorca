package model

// BrowserPluginID is the Browser plugin. Every Runner has it; a bot's Access has one switch for
// Browser, whichever browser the Runner uses.
const BrowserPluginID = "playwright"

// The browsers a Runner can use: ego (lite)'s when it is installed (auto), ego's, or the Browser
// plugin Lorca carries.
const (
	BrowserAuto       = "auto"
	BrowserEgo        = "ego"
	BrowserPlaywright = "playwright"
)

// BrowserEngine is this computer's browser setting and what it comes to.
type BrowserEngine struct {
	// Engine is the setting: auto, ego, or playwright.
	Engine string `json:"engine"`
	// Effective is the browser in use: ego or playwright.
	Effective    string `json:"effective"`
	EgoInstalled bool   `json:"ego_installed"`
}

// BrowserEngine asks this computer's CLI which browser its bots use.
func (s *Store) BrowserEngine(done func(BrowserEngine, error)) {
	if s.IsMock {
		s.post(func() { done(s.mockEngine(), nil) })
		return
	}
	Async(s, func() (BrowserEngine, error) {
		return call[BrowserEngine](s, "browser.engine", map[string]any{})
	}, done)
}

// SetBrowserEngine changes the setting to auto, ego, or playwright.
func (s *Store) SetBrowserEngine(engine string, done func(BrowserEngine, error)) {
	if s.IsMock {
		s.mockBrowserEngine = engine
		s.post(func() { done(s.mockEngine(), nil) })
		return
	}
	Async(s, func() (BrowserEngine, error) {
		return call[BrowserEngine](s, "browser.set_engine", map[string]any{"engine": engine})
	}, done)
}

// mockEngine is the demo's browser: ego (lite) is installed there.
func (s *Store) mockEngine() BrowserEngine {
	engine := s.mockBrowserEngine
	if engine == "" {
		engine = BrowserAuto
	}
	effective := BrowserEgo
	if engine == BrowserPlaywright {
		effective = BrowserPlaywright
	}
	return BrowserEngine{Engine: engine, Effective: effective, EgoInstalled: true}
}
