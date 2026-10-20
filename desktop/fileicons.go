package main

import (
	"embed"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// File icons: a file in a chat, an output's card, and the viewer show what a file is as Untitled
// UI's solid file icon (assets/fileicons, MIT): a page in the format's color with its extension on
// it, or a page with a glyph for a kind that has no icon of its own (code, a document, a picture, a
// recording, a table).

//go:embed assets/fileicons/*.svg
var fileIconFiles embed.FS

// fileIconAliases are the extensions drawn as another's icon, or as their kind's.
var fileIconAliases = map[string]string{
	"htm": "html", "xhtml": "html", "tif": "tiff", "jsx": "js", "mjs": "js", "cjs": "js", "scss": "css", "less": "css",
	"jsonl": "json", "tsv": "spreadsheets", "numbers": "spreadsheets", "ods": "spreadsheets",
	"md": "document", "markdown": "document", "mdx": "document", "rtf": "document", "pages": "document", "odt": "document",
	"tex": "document", "rst": "document", "adoc": "document", "epub": "document",
	"log": "txt", "srt": "txt", "vtt": "txt", "key": "pptx", "odp": "pptx",
	"bmp": "image", "heic": "image", "heif": "image", "avif": "image", "ico": "image", "raw": "image",
	"m4a": "audio", "aac": "audio", "flac": "audio", "ogg": "audio", "oga": "audio", "opus": "audio", "aiff": "audio",
	"mov": "video", "m4v": "video", "webm": "video", "wmv": "video",
	"gz": "zip", "tgz": "zip", "tar": "zip", "7z": "zip", "bz2": "zip", "xz": "zip",
	"pkg": "dmg", "msi": "exe", "app": "dmg", "deb": "exe", "rpm": "exe", "apk": "exe", "ipa": "exe",
	"sketch": "fig", "xd": "fig", "atom": "rss",
}

// fileIconCode are the extensions of source and configuration, drawn as code.
var fileIconCode = map[string]bool{
	"ts": true, "tsx": true, "go": true, "rs": true, "py": true, "rb": true, "swift": true, "kt": true, "c": true,
	"h": true, "cc": true, "cpp": true, "hpp": true, "cs": true, "php": true, "lua": true, "sh": true, "zsh": true,
	"bash": true, "ps1": true, "diff": true, "patch": true, "yaml": true, "yml": true, "toml": true, "ini": true,
	"conf": true, "env": true, "properties": true, "vue": true, "svelte": true, "graphql": true, "proto": true,
	"dockerfile": true, "makefile": true, "gradle": true, "lock": true, "ipynb": true, "wasm": true,
}

// fileIconName is the icon of a file of this name and media type: its extension's own, an alias's,
// its kind's, or the empty page.
func fileIconName(name, mime string) string {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	if ext == "" {
		ext = strings.ToLower(name)
	}
	if alias, ok := fileIconAliases[ext]; ok {
		return alias
	}
	if fileIconCode[ext] {
		return "code"
	}
	if _, err := fileIconFiles.Open("assets/fileicons/" + ext + ".svg"); err == nil && ext != "" {
		return ext
	}
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case mime == "application/pdf":
		return "pdf"
	case mime == "application/json":
		return "json"
	case strings.HasPrefix(mime, "text/"):
		return "txt"
	}
	return "empty"
}

// fileIcon is a parsed icon and the color of its page.
type fileIcon struct {
	svg  *ui.SVG
	tint ui.Color
}

var (
	fileIcons     sync.Map // icon name → *fileIcon
	fileIconColor = regexp.MustCompile(`fill="(#[0-9A-Fa-f]{6})"`)
)

// fileIconFor is the icon of a file, parsed once.
func fileIconFor(name, mime string) *fileIcon {
	key := fileIconName(name, mime)
	if found, ok := fileIcons.Load(key); ok {
		return found.(*fileIcon)
	}
	data, err := fileIconFiles.ReadFile("assets/fileicons/" + key + ".svg")
	if err != nil {
		data, _ = fileIconFiles.ReadFile("assets/fileicons/empty.svg")
	}
	icon := &fileIcon{svg: ui.MustParseSVG(data), tint: ui.Hex("#7F56D9")}
	if m := fileIconColor.FindSubmatch(data); m != nil {
		icon.tint = ui.Hex(string(m[1]))
	}
	fileIcons.Store(key, icon)
	return icon
}

// fileIconView draws a file's icon `size` DIPs square, in its own colors.
func fileIconView(c *ui.Context, name, mime string, size float32) ui.Element {
	return ui.Image(c, fileIconFor(name, mime).svg).Size(size, size)
}
