//! Source chips: a web link in a bot's message reads as its site's icon and the link's words on a
//! rounded fill. These are the rules the phone's views draw by, the desktop app's
//! (`desktop/model/sourcelink.go`): which links are chips, the words on one, and the sites a
//! message names, whose icons the app asks the CLI for.

use crate::{parse, Block, Span};

/// The longest label a chip shows before it ends in an ellipsis.
const LABEL_CHARS: usize = 28;

/// What a chip shows: the site whose icon goes in front, and the words after it.
#[derive(Debug, Clone, PartialEq, Eq, uniffi::Record)]
pub struct SourceChip {
    pub host: String,
    pub label: String,
}

/// The host a web link is shown under, lower case and without a leading `www.`; `None` for a
/// link that is not http(s) with a host, which stays plain link text.
pub fn source_host(href: &str) -> Option<String> {
    let (scheme, rest) = href.split_once("://")?;
    if !scheme.eq_ignore_ascii_case("http") && !scheme.eq_ignore_ascii_case("https") {
        return None;
    }
    let authority = rest.split(['/', '?', '#']).next().unwrap_or("");
    let host_port = authority.rsplit_once('@').map_or(authority, |(_, host)| host);
    let host = if let Some(bracketed) = host_port.strip_prefix('[') {
        bracketed.split(']').next().unwrap_or("")
    } else {
        host_port.split(':').next().unwrap_or("")
    };
    if host.is_empty() || host.chars().any(|c| c.is_whitespace()) {
        return None;
    }
    let host = host.to_ascii_lowercase();
    Some(host.strip_prefix("www.").map(str::to_owned).unwrap_or(host))
}

/// The words on a chip: the link's own words when they say more than its address does, else its
/// site. A bare address, a number such as `[1]`, and an empty text all read as the site.
pub fn source_label(text: &str, href: &str) -> String {
    let host = source_host(href).unwrap_or_default();
    let text = text.split_whitespace().collect::<Vec<_>>().join(" ");
    if text.is_empty() || text == href || source_host(&text).is_some() || text.to_ascii_lowercase().starts_with("www.") || is_citation_mark(&text) {
        return host;
    }
    if text.chars().count() > LABEL_CHARS {
        let cut: String = text.chars().take(LABEL_CHARS - 1).collect();
        return format!("{}…", cut.trim_end());
    }
    text
}

/// A link's text that is only a number, such as `[1]` or `1`.
fn is_citation_mark(text: &str) -> bool {
    let text = text.trim_matches(|c| matches!(c, '[' | ']' | '(' | ')' | ' '));
    !text.is_empty() && text.bytes().all(|b| b.is_ascii_digit())
}

/// The chip for a link's words, or `None` when the link stays link text.
#[uniffi::export]
pub fn source_chip(text: String, href: String) -> Option<SourceChip> {
    let host = source_host(&href)?;
    Some(SourceChip { label: source_label(&text, &href), host })
}

/// The runs of one web link, which styled words inside it split, as one span, so the link makes
/// one chip. Other spans stay as they are.
#[uniffi::export]
pub fn join_link_spans(spans: Vec<Span>) -> Vec<Span> {
    let mut out: Vec<Span> = Vec::with_capacity(spans.len());
    for span in spans {
        if let (Some(last), Some(link)) = (out.last_mut(), span.link.as_deref()) {
            if last.link.as_deref() == Some(link) && source_host(link).is_some() {
                last.text.push_str(&span.text);
                continue;
            }
        }
        out.push(span);
    }
    out
}

/// The sites a message's chips name, each once, in the order they appear.
#[uniffi::export]
pub fn source_hosts(markdown: String) -> Vec<String> {
    let mut hosts = Vec::new();
    for block in parse(&markdown).blocks {
        collect_block(&block, &mut hosts);
    }
    hosts
}

fn collect_block(block: &Block, hosts: &mut Vec<String>) {
    match block {
        Block::Paragraph { spans } | Block::Heading { spans, .. } => collect_spans(spans, hosts),
        Block::Listing { items, .. } => items.iter().flat_map(|item| &item.blocks).for_each(|b| collect_block(b, hosts)),
        Block::Quote { blocks } => blocks.iter().for_each(|b| collect_block(b, hosts)),
        Block::Table { header, rows, .. } => {
            header.iter().chain(rows.iter().flat_map(|row| &row.cells)).for_each(|cell| collect_spans(&cell.spans, hosts))
        }
        Block::Code { .. } | Block::Rule => {}
    }
}

fn collect_spans(spans: &[Span], hosts: &mut Vec<String>) {
    for host in spans.iter().filter_map(|span| span.link.as_deref().and_then(source_host)) {
        if !hosts.contains(&host) {
            hosts.push(host);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn labels_say_more_than_the_address() {
        let cases = [
            ("the guide", "https://www.example.com/guide", "the guide"),
            ("https://docs.rs/serde", "https://docs.rs/serde", "docs.rs"),
            ("www.example.com/a", "http://www.example.com/a", "example.com"),
            ("[1]", "https://a.dev/x", "a.dev"),
            ("2", "https://a.dev/x", "a.dev"),
            ("", "https://a.dev/x", "a.dev"),
            ("  two\n words ", "https://a.dev/x", "two words"),
            ("A very long title that would make the chip wider than a line", "https://a.dev/x", "A very long title that woul…"),
        ];
        for (text, href, want) in cases {
            assert_eq!(source_label(text, href), want, "{text:?} {href:?}");
        }
    }

    #[test]
    fn only_web_links_with_a_host_are_chips() {
        let cases = [
            ("https://www.Example.com/a?b=1", Some("example.com")),
            ("http://docs.rs:8080/x", Some("docs.rs")),
            ("https://github.com", Some("github.com")),
            ("https://user:secret@a.dev/x", Some("a.dev")),
            ("mailto:a@b.co", None),
            ("file:///etc/passwd", None),
            ("https:///nohost", None),
            ("lorca://chat/1", None),
            ("not a url", None),
        ];
        for (href, want) in cases {
            assert_eq!(source_host(href).as_deref(), want, "{href:?}");
        }
    }

    #[test]
    fn styled_words_in_one_link_make_one_chip() {
        let doc = parse("See [**the** guide](https://a.dev/g) and [docs](https://b.dev).");
        let Block::Paragraph { spans } = &doc.blocks[0] else { panic!("{doc:?}") };
        let joined = join_link_spans(spans.clone());
        let links: Vec<_> = joined.iter().filter(|s| s.link.is_some()).map(|s| s.text.as_str()).collect();
        assert_eq!(links, ["the guide", "docs"]);
    }

    #[test]
    fn a_message_names_each_site_once() {
        let text = "Read [one](https://a.dev/1), <https://www.a.dev/2>, and https://docs.rs/x.\n\n- [b](http://b.dev)\n\n> [mail](mailto:x@y.z)\n\n```\nhttps://c.dev\n```";
        assert_eq!(source_hosts(text.into()), ["a.dev", "docs.rs", "b.dev"]);
    }
}
