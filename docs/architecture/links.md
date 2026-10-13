# Web links in messages

A web link in a bot's message reads as a **source chip**: the site's icon and the link's words on a rounded fill, inside the paragraph. The desktop app draws them; the link itself is the Markdown link or bare address the bot wrote, and clicking a chip opens it as the link did (`openExternal` refuses anything but http(s) and mailto, [Desktop app](desktop-app.md)).

## The chip

- **Which links**: an `http` or `https` link with a host (`model.SourceHost`, `desktop/model/sourcelink.go`). Mail links, `lorca://` links, and anything else stay link text. The user's own bubble keeps its links as text, since the chip's fill is not for the accent bubble.
- **Its words** (`model.SourceLabel`): the link's own words when they say more than its address does, cut at 28 characters; the site's host, without `www.`, when the text is the address itself, a number such as `[1]`, or empty. Styled words inside one link (`[**bold** text](url)`) make one chip (`joinLinkSpans`).
- **Drawing** (`desktop/sources.go`): MyGo's paragraphs hold only text, so a chip is text with a no-break space and two figure spaces ahead of its label, where the icon goes. The paragraph's `Draw` paints, under its words, the fill with a hairline (the card colors, so it stands out of a bubble's gray) and the icon, at the rectangle MyGo reports for the chip's words (`Bounds`, the last frame's; a chip new to the screen asks for the next frame). The label's spaces do not break, so a chip never splits across lines. The fill and the icon sit a point under the middle of the line's box, where the words' weight is.
- **The icon**: the site's favicon once the CLI has found it, else a globe. Each site is asked about once per run. An ICO file is turned into the PNG of its best image (`model.FaviconImage`: the smallest at least 32 px, PNG or 24/32-bit bitmap entries), since not every platform's decoder reads ICO.

## `links.favicon`

The app speaks only to its CLI, so the CLI fetches the icon (`crates/cli/src/favicon.rs`). `links.favicon { host }` answers `{ icon: { mime, data } | null }`, `data` being base64.

- The host is a public site name: dotted labels of letters, digits, and hyphens; never an address or a single word like `localhost`.
- The CLI resolves the host and refuses any address that is loopback, private, link-local, shared, or multicast; the connection then goes to the addresses it checked, over HTTPS, so a name that changes its answer in between reaches nothing else. Redirects stay on the same host.
- It asks for `/favicon.ico`, then reads the site's page for `rel="icon"` and `apple-touch-icon` links on the same host (the first three). An icon is at most 64 KiB and passes only as PNG, ICO, JPEG, GIF, or WebP by its first bytes, never by its header and never SVG.
- It keeps the answer in `favicons/` under the data directory, an icon for 7 days and a miss for one.

The demo (`LORCA_MOCK=1`) has no CLI to ask; `model/mockicons/` holds the real favicons of a few sites (GitHub Docs, docs.rs, Linear), and the rest stay on the globe. Its Researcher chat has a message with links to both.
