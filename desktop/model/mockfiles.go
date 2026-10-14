package model

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The files Researcher's outputs carry in the demo (`LORCA_MOCK=1`): a Markdown note, an HTML page
// with a script and a link, a PDF, a diagram, and a long log. They are made when first needed and
// written under a temporary folder, where the store finds them as the CLI's `files.path` would.

type mockFile struct {
	id, name, mime string
	data           []byte
	width, height  int
}

var mockFiles = sync.OnceValue(func() []mockFile {
	picture, width, height := mockDiagram()
	chart, chartWidth, chartHeight := mockChart()
	return []mockFile{
		{id: "chart", name: "Setup funnel.png", mime: "image/png", data: chart, width: chartWidth, height: chartHeight},
		{id: "screenshot", name: "Pairing screen.png", mime: "image/png", data: picture, width: width, height: height},
		{id: "notes", name: "Onboarding notes.md", mime: "text/markdown", data: []byte(mockNotesMarkdown)},
		{id: "page", name: "Setup comparison.html", mime: "text/html", data: []byte(mockComparisonHTML)},
		{id: "pdf", name: "Setup comparison.pdf", mime: "application/pdf", data: mockPDF()},
		{id: "diagram", name: "Device roles.png", mime: "image/png", data: picture, width: width, height: height},
		{id: "log", name: "Link check.log", mime: "text/plain", data: []byte(mockLinkLog())},
	}
})

// mockFilePaths writes the demo's files once and answers where each attachment id's bytes are.
var mockFilePaths = sync.OnceValue(func() map[string]string {
	paths := map[string]string{}
	dir, err := os.MkdirTemp("", "lorca-demo-files-")
	if err != nil {
		return paths
	}
	for _, file := range mockFiles() {
		path := filepath.Join(dir, file.name)
		if os.WriteFile(path, file.data, 0o600) == nil {
			paths["att-mock-"+file.id] = path
		}
	}
	return paths
})

// seedMockFiles tells the store where the demo's files are.
func (s *Store) seedMockFiles() {
	for id, path := range mockFilePaths() {
		s.attachmentFiles[id] = path
	}
}

// mockOutputMessages are Researcher's published files, in the order it made them: the note, then
// a comparison as a page and a PDF beside a diagram, then a check with its result.
func mockOutputMessages() []*Message {
	output := func(id string, minutes float64, evidence *OutputEvidence) *Message {
		var file mockFile
		for _, each := range mockFiles() {
			if each.id == id {
				file = each
			}
		}
		message := mockMessage(BotAuthor("bot-scout"), textBody(""), minutesAgo(minutes))
		message.Attachments = []Attachment{{ID: "att-mock-" + id, Name: file.name, Mime: file.mime, Size: int64(len(file.data)), Width: file.width, Height: file.height}}
		message.Output = &Output{ID: "out-mock-" + id, Name: file.name, Mime: file.mime, BotID: "bot-scout", Version: 1, Evidence: evidence}
		return message
	}
	return []*Message{
		output("notes", 41, nil),
		mockMessage(BotAuthor("bot-scout"), textBody("I also compared how five similar apps explain their first steps."), minutesAgo(40)),
		output("page", 39.6, nil),
		output("pdf", 39.5, nil),
		output("diagram", 39.4, nil),
		mockMessage(BotAuthor("bot-scout"), textBody("I checked the links in the setup guide too."), minutesAgo(38.5)),
		output("log", 38, &OutputEvidence{Kind: "test_result", Status: "passed", Summary: "All 41 links in the setup guide answer.", Command: "bun run check:links"}),
		mockMessage(BotAuthor("bot-scout"), textBody("Here is where people drop off during setup, by step."), minutesAgo(37.5)),
		// A picture of its own, large enough to need the lightbox's pinch and slide.
		output("chart", 37, nil),
		mockSetupScreenshot(),
	}
}

// mockSetupScreenshot is a picture the user sent: an image attachment on a message of their own.
func mockSetupScreenshot() *Message {
	var file mockFile
	for _, each := range mockFiles() {
		if each.id == "screenshot" {
			file = each
		}
	}
	message := mockMessage(You, textBody("Is this the screen you walked through?"), minutesAgo(35.5))
	message.Attachments = []Attachment{{ID: "att-mock-screenshot", Name: file.name, Mime: file.mime, Size: int64(len(file.data)), Width: file.width, Height: file.height}}
	return message
}

const mockNotesMarkdown = "# Onboarding notes\n\n" +
	"What a new user should hear **right after pairing**, from reading the setup guide as one.\n\n" +
	"## What to explain sooner\n\n" +
	"1. Your computer runs the bots.\n" +
	"2. Your phone lets you chat with them.\n" +
	"3. A bot works on one computer, its *Runner*.\n\n" +
	"> Keep the first screen short. People read the first two lines and decide.\n\n" +
	"## Roles\n\n" +
	"| Device | Runs bots | Chats |\n" +
	"| --- | --- | --- |\n" +
	"| Computer | Yes | Yes |\n" +
	"| Phone | No | Yes |\n\n" +
	"## Pairing\n\n" +
	"Run this on the computer, then scan the code on the phone:\n\n" +
	"```sh\nlorca pair\n```\n\n" +
	"See the [pairing guide](https://lorca.app/docs/pairing) for the whole walkthrough.\n\n" +
	"---\n\n" +
	"## Snippets\n\n" +
	"### The welcome card\n\n" +
	"```html\n<!-- shown right after pairing -->\n<section class=\"welcome\" data-step=\"1\">\n  <h1 id=\"title\">Your computer runs the bots</h1>\n  <button onclick=\"next()\">Continue</button>\n</section>\n```\n\n" +
	"### Tracking the step\n\n" +
	"```js\n// Move to the next step, at most 3.\nfunction next(step = 0) {\n  const label = \"Step \" + (step + 1);\n  return step < 3 ? label : null;\n}\n```\n\n" +
	"### The change\n\n" +
	"```diff\n@@ -1,3 +1,3 @@\n-Pair your phone first.\n+Explain the Device roles first.\n Then pair.\n```\n\n" +
	"- Keep **one** idea per screen\n  - Say who runs the bots\n  - Say who chats with them\n- Put `lorca pair` where people can copy it\n"

const mockComparisonHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Setup comparison</title>
<style>
  body { font: 15px/1.55 -apple-system, "Segoe UI", sans-serif; color: #1d1d1f; margin: 0; padding: 32px; background: #fafafa; }
  h1 { font-size: 22px; margin: 0 0 4px; }
  p.lead { color: #6e6e73; margin: 0 0 20px; }
  table { border-collapse: collapse; width: 100%; background: #fff; border: 1px solid #e5e5ea; }
  th, td { text-align: left; padding: 9px 12px; border-bottom: 1px solid #e5e5ea; }
  th { background: #f2f2f7; font-weight: 600; }
  button { font: inherit; padding: 6px 14px; border-radius: 8px; border: 1px solid #c7c7cc; background: #fff; cursor: pointer; }
  #answer { margin-left: 10px; color: #0a84ff; }
</style>
</head>
<body>
  <h1>What each app explains first</h1>
  <p class="lead">Five apps, compared on their first screen after sign-in.</p>
  <table>
    <tr><th>App</th><th>First thing it explains</th><th>Steps to a first message</th></tr>
    <tr><td>Alpha</td><td>Your workspace</td><td>3</td></tr>
    <tr><td>Beta</td><td>Inviting a teammate</td><td>4</td></tr>
    <tr><td>Gamma</td><td>Connecting an account</td><td>2</td></tr>
    <tr><td>Delta</td><td>A sample project</td><td>3</td></tr>
    <tr><td>Epsilon</td><td>Keyboard shortcuts</td><td>5</td></tr>
  </table>
  <p>
    <button id="run">Which is quickest?</button><span id="answer"></span>
  </p>
  <p>Source notes: <a href="https://example.com/setup-guides">example.com/setup-guides</a></p>
  <script>
    document.getElementById("run").addEventListener("click", function () {
      document.getElementById("answer").textContent = "Gamma, in 2 steps.";
    });
  </script>
</body>
</html>
`

// mockLinkLog is a long check log, so the viewer has something to scroll.
func mockLinkLog() string {
	var log strings.Builder
	log.WriteString("Checking the setup guide's links\n\n")
	for i := 1; i <= 41; i++ {
		fmt.Fprintf(&log, "ok  200  https://lorca.app/docs/setup#step-%d\n", i)
	}
	log.WriteString("\nAll 41 links answer.\n")
	return log.String()
}

// mockDiagram draws two boxes, a computer and a phone, joined by a line.
func mockDiagram() ([]byte, int, int) {
	const width, height = 960, 540
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	fill := func(x0, y0, x1, y1 int, c color.RGBA) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				picture.SetRGBA(x, y, c)
			}
		}
	}
	fill(0, 0, width, height, color.RGBA{250, 250, 252, 255})
	fill(0, 0, width, 64, color.RGBA{41, 92, 242, 255})
	fill(120, 160, 420, 400, color.RGBA{229, 236, 255, 255})
	fill(540, 160, 840, 400, color.RGBA{234, 246, 238, 255})
	fill(420, 276, 540, 284, color.RGBA{140, 140, 150, 255})
	for _, bar := range [][4]int{{160, 220, 380, 236}, {160, 260, 340, 276}, {580, 220, 800, 236}, {580, 260, 760, 276}} {
		fill(bar[0], bar[1], bar[2], bar[3], color.RGBA{190, 198, 220, 255})
	}
	var out bytes.Buffer
	_ = png.Encode(&out, picture)
	return out.Bytes(), width, height
}

// mockChart draws a bar chart of the steps of setup on a fine grid, 1600 by 1000, with detail small
// enough that zooming in has something to show.
func mockChart() ([]byte, int, int) {
	const width, height = 1600, 1000
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	fill := func(x0, y0, x1, y1 int, c color.RGBA) {
		for y := max(y0, 0); y < min(y1, height); y++ {
			for x := max(x0, 0); x < min(x1, width); x++ {
				picture.SetRGBA(x, y, c)
			}
		}
	}
	fill(0, 0, width, height, color.RGBA{255, 255, 255, 255})
	for x := 100; x < width; x += 50 {
		fill(x, 100, x+1, 900, color.RGBA{232, 232, 238, 255})
	}
	for y := 100; y <= 900; y += 50 {
		fill(100, y, 1500, y+1, color.RGBA{232, 232, 238, 255})
	}
	fill(98, 100, 100, 902, color.RGBA{60, 60, 67, 255})
	fill(98, 900, 1500, 902, color.RGBA{60, 60, 67, 255})
	// Each step keeps fewer people than the one before, with a row of ticks above it.
	kept := []int{800, 690, 560, 470, 330, 290, 210, 160}
	for i, value := range kept {
		left := 140 + i*170
		fill(left, 900-value, left+110, 900, color.RGBA{uint8(41 + i*8), uint8(92 + i*10), 242, 255})
		for tick := 0; tick < 11; tick++ {
			fill(left+tick*10, 900-value-12, left+tick*10+2, 900-value-4, color.RGBA{60, 60, 67, 255})
		}
	}
	fill(0, 0, width, 64, color.RGBA{41, 92, 242, 255})
	var out bytes.Buffer
	_ = png.Encode(&out, picture)
	return out.Bytes(), width, height
}

// mockPDF is a one-page PDF with a title and a few lines, its cross-reference table computed from
// where each object lands.
func mockPDF() []byte {
	var text strings.Builder
	text.WriteString("BT /F1 24 Tf 72 700 Td (Setup comparison) Tj ET\n")
	for i, line := range []string{
		"Five apps, compared on their first screen after sign-in.",
		"Gamma reaches a first message in 2 steps, the fewest.",
		"Epsilon needs 5, and opens with keyboard shortcuts.",
	} {
		fmt.Fprintf(&text, "BT /F2 13 Tf 72 %d Td (%s) Tj ET\n", 660-i*22, line)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", text.Len(), text.String()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, object := range objects {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	start := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, start)
	return out.Bytes()
}
