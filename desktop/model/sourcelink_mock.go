package model

import (
	"embed"
	"path"
)

// The demo's site icons are the sites' real favicons, kept in the app so the demo needs no network.
// A site that is not here has no icon, as most on the web.
//
//go:embed mockicons/*.png
var mockIcons embed.FS

// mockFavicon is the PNG of a demo site's icon, or nil when it has none.
func mockFavicon(host string) []byte {
	data, err := mockIcons.ReadFile(path.Join("mockicons", host+".png"))
	if err != nil {
		return nil
	}
	return data
}
