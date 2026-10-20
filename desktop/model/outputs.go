package model

import (
	"log"
	"net/url"
	"path"
	"slices"
	"strings"
)

// Output is a file or document link a bot published in a chat: one version of it. ID names the
// output across its versions; the message that carries it is the version. The app shows the file,
// not what the bot says it checked, so the record's `evidence` is not read.
type Output struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	BotID   string `json:"bot_id"`
	Version uint32 `json:"version"`
	URL     string `json:"url,omitempty"`
}

// DocumentURL is the document a link output points at; only an https address without credentials
// opens.
func (o Output) DocumentURL() string {
	parsed, err := url.Parse(o.URL)
	if o.URL == "" || err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	return parsed.String()
}

// OutputKind is what an output is, for its icon and its tint: a link or a kind of file.
type OutputKind int

const (
	OutputFile OutputKind = iota
	OutputLink
	OutputImage
	OutputVideo
	OutputAudio
	OutputPDF
	OutputCode
	OutputData
	OutputTable
	OutputArchive
	OutputText
)

var outputExtensions = map[string]OutputKind{
	"html": OutputCode, "htm": OutputCode, "css": OutputCode, "js": OutputCode, "mjs": OutputCode, "ts": OutputCode,
	"tsx": OutputCode, "jsx": OutputCode, "go": OutputCode, "rs": OutputCode, "py": OutputCode, "rb": OutputCode,
	"swift": OutputCode, "java": OutputCode, "kt": OutputCode, "c": OutputCode, "h": OutputCode, "cc": OutputCode,
	"cpp": OutputCode, "sh": OutputCode, "sql": OutputCode, "svg": OutputCode, "diff": OutputCode, "patch": OutputCode,
	"json": OutputData, "jsonl": OutputData, "yaml": OutputData, "yml": OutputData, "toml": OutputData, "xml": OutputData,
	"csv": OutputTable, "tsv": OutputTable, "xls": OutputTable, "xlsx": OutputTable, "numbers": OutputTable,
	"zip": OutputArchive, "gz": OutputArchive, "tgz": OutputArchive, "tar": OutputArchive, "7z": OutputArchive, "rar": OutputArchive,
	"txt": OutputText, "md": OutputText, "log": OutputText, "rtf": OutputText, "doc": OutputText, "docx": OutputText,
}

// Kind is what the output is, from its link, its media type, or its file's extension.
func (o Output) Kind() OutputKind {
	if o.URL != "" {
		return OutputLink
	}
	if kind, ok := outputExtensions[strings.ToLower(strings.TrimPrefix(path.Ext(o.Name), "."))]; ok {
		return kind
	}
	switch {
	case strings.HasPrefix(o.Mime, "image/"):
		return OutputImage
	case strings.HasPrefix(o.Mime, "video/"):
		return OutputVideo
	case strings.HasPrefix(o.Mime, "audio/"):
		return OutputAudio
	case o.Mime == "application/pdf":
		return OutputPDF
	case o.Mime == "application/json":
		return OutputData
	case strings.HasPrefix(o.Mime, "text/"):
		return OutputText
	}
	return OutputFile
}

// Symbol is the SF Symbol name for what the output is.
func (o Output) Symbol() string {
	switch o.Kind() {
	case OutputLink:
		return "link"
	case OutputImage:
		return "doc.image"
	case OutputVideo:
		return "doc.play"
	case OutputAudio:
		return "doc.music"
	case OutputPDF:
		return "doc.richtext"
	case OutputCode:
		return "doc.code"
	case OutputData:
		return "doc.braces"
	case OutputTable:
		return "doc.table"
	case OutputArchive:
		return "doc.zipper"
	case OutputText:
		return "doc.text"
	}
	return "doc"
}

// PreviewKind is how the viewer shows a file: rendered, or as the words it holds. A table is a
// CSV or TSV file drawn as a grid; media is a recording the web view plays; an SVG is a vector
// picture the web view draws, with its source a switch away.
type PreviewKind int

const (
	PreviewNone PreviewKind = iota
	PreviewImage
	PreviewMarkdown
	PreviewHTML
	PreviewPDF
	PreviewText
	PreviewTable
	PreviewMedia
	PreviewSVG
)

var previewExtensions = map[string]PreviewKind{
	"png": PreviewImage, "jpg": PreviewImage, "jpeg": PreviewImage, "gif": PreviewImage, "webp": PreviewImage, "bmp": PreviewImage,
	"md": PreviewMarkdown, "markdown": PreviewMarkdown, "mdx": PreviewMarkdown,
	"html": PreviewHTML, "htm": PreviewHTML, "xhtml": PreviewHTML,
	"pdf": PreviewPDF,
	"csv": PreviewTable, "tsv": PreviewTable,
	"svg": PreviewSVG,
	"mp3": PreviewMedia, "wav": PreviewMedia, "m4a": PreviewMedia, "aac": PreviewMedia, "flac": PreviewMedia, "ogg": PreviewMedia,
	"oga": PreviewMedia, "opus": PreviewMedia, "mp4": PreviewMedia, "m4v": PreviewMedia, "mov": PreviewMedia, "webm": PreviewMedia,
}

// textExtensions are the files whose words the viewer shows as they are.
var textExtensions = map[string]bool{
	"txt": true, "log": true, "json": true, "jsonl": true, "yaml": true, "yml": true, "toml": true, "xml": true,
	"css": true, "scss": true, "less": true, "js": true, "mjs": true, "cjs": true, "ts": true, "tsx": true, "jsx": true,
	"go": true, "rs": true, "py": true, "rb": true, "swift": true, "java": true, "kt": true, "c": true, "h": true,
	"cc": true, "cpp": true, "hpp": true, "cs": true, "php": true, "lua": true, "sh": true, "zsh": true, "bash": true,
	"ps1": true, "sql": true, "diff": true, "patch": true, "ini": true, "conf": true, "env": true, "properties": true,
	"vue": true, "svelte": true, "graphql": true, "proto": true, "dockerfile": true, "makefile": true, "gradle": true,
	"lock": true, "srt": true, "vtt": true, "tex": true, "rst": true, "adoc": true,
}

// PreviewKindOf is how a file of this name and media type is shown: its extension decides, then its
// media type.
func PreviewKindOf(name, mime string) PreviewKind {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	if kind, ok := previewExtensions[ext]; ok {
		return kind
	}
	if textExtensions[ext] {
		return PreviewText
	}
	switch {
	case mime == "image/svg+xml":
		return PreviewSVG
	case mime == "text/csv", mime == "text/tab-separated-values":
		return PreviewTable
	case strings.HasPrefix(mime, "audio/"), strings.HasPrefix(mime, "video/"):
		return PreviewMedia
	case strings.HasPrefix(mime, "image/"):
		return PreviewImage
	case mime == "application/pdf":
		return PreviewPDF
	case mime == "text/html":
		return PreviewHTML
	case mime == "text/markdown":
		return PreviewMarkdown
	case strings.HasPrefix(mime, "text/"), mime == "application/json", mime == "application/xml":
		return PreviewText
	}
	return PreviewNone
}

// PreviewKind is how the viewer shows the attachment.
func (a Attachment) PreviewKind() PreviewKind { return PreviewKindOf(a.Name, a.Mime) }

// TypeName is "HTML", "PNG", or the link's host: what the output's card says it is beside its size.
func (o Output) TypeName() string {
	if o.URL != "" {
		if parsed, err := url.Parse(o.URL); err == nil {
			return strings.TrimPrefix(parsed.Hostname(), "www.")
		}
		return ""
	}
	if ext := strings.TrimPrefix(path.Ext(o.Name), "."); ext != "" && len(ext) <= 8 {
		return strings.ToUpper(ext)
	}
	if _, sub, ok := strings.Cut(o.Mime, "/"); ok && sub != "octet-stream" && len(sub) <= 8 {
		return strings.ToUpper(sub)
	}
	return ""
}

// OutputSeries is an output and its versions, newest first. Each version is a message of its own.
type OutputSeries struct {
	Versions []*Message
}

func (s OutputSeries) ID() string       { return s.Versions[0].Output.ID }
func (s OutputSeries) Latest() *Message { return s.Versions[0] }
func (s OutputSeries) Output() *Output  { return s.Versions[0].Output }

// GroupOutputs sorts a chat's output messages into one series per output, the latest published
// first.
func GroupOutputs(messages []*Message) []OutputSeries {
	var order []string
	byID := map[string][]*Message{}
	for _, message := range messages {
		if message.Output == nil {
			continue
		}
		id := message.Output.ID
		if _, ok := byID[id]; !ok {
			order = append(order, id)
		}
		byID[id] = append(byID[id], message)
	}
	series := make([]OutputSeries, 0, len(order))
	for _, id := range order {
		versions := slices.Clone(byID[id])
		slices.SortStableFunc(versions, func(a, b *Message) int { return int(b.Output.Version) - int(a.Output.Version) })
		series = append(series, OutputSeries{Versions: versions})
	}
	slices.SortStableFunc(series, func(a, b OutputSeries) int { return b.Latest().CreatedAt.Compare(a.Latest().CreatedAt) })
	return series
}

// Outputs are the chat's outputs, the latest published first: what `outputs.list` answered and
// output messages that arrived since. A chat is asked for when something first shows it, and again
// after a resync while what it had stays on screen; until the CLI answers there are none.
func (s *Store) Outputs(chatID string) []OutputSeries {
	if s.IsMock {
		if chat := s.Chat(chatID); chat != nil {
			return GroupOutputs(chat.Messages)
		}
		return nil
	}
	known, ok := s.outputMessages[chatID]
	if !ok || s.staleOutputs[chatID] {
		s.listOutputs(chatID)
	}
	return GroupOutputs(known)
}

func (s *Store) listOutputs(chatID string) {
	if s.outputRequests[chatID] {
		return
	}
	s.outputRequests[chatID] = true
	delete(s.staleOutputs, chatID)
	identity := s.IdentityID
	Async(s, func() ([]WireMessage, error) {
		reply, err := call[struct {
			Outputs []WireMessage `json:"outputs"`
		}](s, "outputs.list", map[string]any{"chat_id": chatID})
		return reply.Outputs, err
	}, func(wire []WireMessage, err error) {
		delete(s.outputRequests, chatID)
		if identity != s.IdentityID || s.Chat(chatID) == nil {
			return
		}
		if err != nil {
			log.Printf("listing outputs failed: %s", ErrorText(err))
			return
		}
		messages := make([]*Message, 0, len(wire))
		for _, each := range wire {
			messages = append(messages, ToMessage(each))
		}
		// Output messages that arrived while the list was on its way are in it too.
		for _, message := range s.outputMessages[chatID] {
			if !slices.ContainsFunc(messages, func(m *Message) bool { return m.ID == message.ID }) {
				messages = append(messages, message)
			}
		}
		s.outputMessages[chatID] = messages
		s.emit(Event{Kind: EventOutputsChanged, ChatID: chatID})
	})
}

// noteOutput keeps a chat's known outputs in step with a message that was added, changed, or (nil,
// with its id) removed.
func (s *Store) noteOutput(message *Message, removed, chatID string) {
	messages, ok := s.outputMessages[chatID]
	if !ok {
		return
	}
	id := removed
	if message != nil {
		id = message.ID
	}
	index := slices.IndexFunc(messages, func(m *Message) bool { return m.ID == id })
	if index < 0 && (message == nil || message.Output == nil) {
		return
	}
	next := slices.Clone(messages)
	if index >= 0 {
		next = slices.Delete(next, index, index+1)
	}
	if message != nil && message.Output != nil {
		next = append(next, message)
	}
	s.outputMessages[chatID] = next
	s.emit(Event{Kind: EventOutputsChanged, ChatID: chatID})
}

// AttachmentError is why the attachment's bytes could not be fetched, until a retry.
func (s *Store) AttachmentError(id string) string { return s.attachmentErrors[id] }

// RetryAttachment fetches a file whose fetch failed again.
func (s *Store) RetryAttachment(attachment Attachment, chatID, messageID string) {
	if _, failed := s.attachmentErrors[attachment.ID]; !failed {
		return
	}
	delete(s.attachmentErrors, attachment.ID)
	s.LocalFile(attachment, chatID, messageID)
	s.emit(Event{Kind: EventMessageChanged, ChatID: chatID, MessageID: messageID})
}

// OpenableFile is the attachment as a file named for what it is, for another app: the bytes under
// their attachment id carry no extension, so the CLI keeps a named copy. done runs on the main
// thread.
func (s *Store) OpenableFile(attachment Attachment, done func(string, error)) {
	if path := s.attachmentFiles[attachment.ID]; path != "" && strings.Contains(path[max(0, strings.LastIndexAny(path, `/\`)):], ".") {
		done(path, nil)
		return
	}
	Async(s, func() (string, error) {
		reply, err := call[struct {
			Path string `json:"path"`
		}](s, "files.path", map[string]any{"attachment": map[string]any{"id": attachment.ID, "name": attachment.Name, "mime": attachment.Mime, "size": attachment.Size}, "named": true})
		if err == nil && reply.Path == "" {
			err = &RequestError{L("File unavailable")}
		}
		return reply.Path, err
	}, done)
}
