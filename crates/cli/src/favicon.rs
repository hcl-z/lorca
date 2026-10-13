//! Site icons for the source chips under a bot's message. The apps speak only to this CLI, so
//! it fetches an icon for them: over HTTPS from the site's own host, never from an address on
//! the local network, and keeps what it found (or that it found nothing) for a while.

use std::net::{IpAddr, SocketAddr};
use std::path::Path;
use std::sync::Arc;
use std::time::{Duration, SystemTime};

use base64::Engine;
use reqwest::header::LOCATION;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};

use crate::app::App;

const MAX_ICON_BYTES: usize = 64 * 1024;
const MAX_PAGE_BYTES: usize = 128 * 1024;
const MAX_REDIRECTS: usize = 3;
const FOUND_TTL: Duration = Duration::from_secs(7 * 24 * 3600);
const MISSING_TTL: Duration = Duration::from_secs(24 * 3600);

/// `links.favicon { host }` answers `{ icon: { mime, data } | null }`, `data` being base64.
pub async fn dispatch(app: &Arc<App>, params: &Value) -> Result<Value, String> {
    let host = valid_host(params["host"].as_str().unwrap_or("")).ok_or("Not a site name")?;
    let path = app.config.home.join("favicons").join(cache_name(&host));
    let icon = match read_cache(&path) {
        Some(cached) => cached,
        None => {
            let found = fetch(&host).await.ok();
            write_cache(&path, found.as_ref());
            found
        }
    };
    Ok(match icon {
        Some((mime, bytes)) => json!({ "icon": { "mime": mime, "data": base64::engine::general_purpose::STANDARD.encode(bytes) } }),
        None => json!({ "icon": null }),
    })
}

/// A public site name in lower case: dotted labels of letters, digits, and hyphens, which is
/// neither an address nor a single word like `localhost`.
fn valid_host(host: &str) -> Option<String> {
    let host = host.trim().trim_end_matches('.').to_ascii_lowercase();
    if host.is_empty() || host.len() > 253 {
        return None;
    }
    let labels: Vec<&str> = host.split('.').collect();
    if labels.len() < 2 || labels.last()?.bytes().all(|b| b.is_ascii_digit()) {
        return None;
    }
    let label_ok = |label: &&str| {
        !label.is_empty()
            && label.len() <= 63
            && !label.starts_with('-')
            && !label.ends_with('-')
            && label.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-')
    };
    labels.iter().all(label_ok).then_some(host)
}

fn is_public(ip: IpAddr) -> bool {
    match ip {
        IpAddr::V4(v4) => {
            let o = v4.octets();
            !(v4.is_private()
                || v4.is_loopback()
                || v4.is_link_local()
                || v4.is_unspecified()
                || v4.is_broadcast()
                || v4.is_multicast()
                || v4.is_documentation()
                || o[0] == 0
                || (o[0] == 100 && o[1] & 0xC0 == 64)
                || (o[0] == 198 && o[1] & 0xFE == 18))
        }
        IpAddr::V6(v6) => {
            if let Some(v4) = v6.to_ipv4_mapped() {
                return is_public(IpAddr::V4(v4));
            }
            let s = v6.segments();
            !(v6.is_loopback() || v6.is_unspecified() || v6.is_multicast() || s[0] & 0xfe00 == 0xfc00 || s[0] & 0xffc0 == 0xfe80)
        }
    }
}

/// The image kind from its first bytes, for the formats the apps decode. A page, or SVG, which
/// can carry script, is none of them.
fn sniff(bytes: &[u8]) -> Option<&'static str> {
    match bytes {
        [0x89, b'P', b'N', b'G', ..] => Some("image/png"),
        [0, 0, 1, 0, ..] => Some("image/x-icon"),
        [0xFF, 0xD8, 0xFF, ..] => Some("image/jpeg"),
        [b'G', b'I', b'F', b'8', ..] => Some("image/gif"),
        [b'R', b'I', b'F', b'F', _, _, _, _, b'W', b'E', b'B', b'P', ..] => Some("image/webp"),
        _ => None,
    }
}

/// The `href` of each `<link rel="…icon…">` in a page, in order.
fn icon_links(page: &str) -> Vec<String> {
    use std::sync::OnceLock;
    static TAG: OnceLock<regex::Regex> = OnceLock::new();
    static REL: OnceLock<regex::Regex> = OnceLock::new();
    static HREF: OnceLock<regex::Regex> = OnceLock::new();
    let tag = TAG.get_or_init(|| regex::Regex::new(r"(?is)<link\b[^>]*>").unwrap());
    let rel = REL.get_or_init(|| regex::Regex::new(r#"(?is)\brel\s*=\s*(?:"([^"]*)"|'([^']*)')"#).unwrap());
    let href = HREF.get_or_init(|| regex::Regex::new(r#"(?is)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)')"#).unwrap());
    let value = |captures: regex::Captures| captures.get(1).or(captures.get(2)).map(|m| m.as_str().trim().to_string());
    tag.find_iter(page)
        .filter_map(|tag| {
            let tag = tag.as_str();
            let rel = rel.captures(tag).and_then(value)?.to_ascii_lowercase();
            if !rel.split_whitespace().any(|word| word == "icon" || word == "apple-touch-icon") {
                return None;
            }
            href.captures(tag).and_then(value).filter(|href| !href.is_empty())
        })
        .collect()
}

async fn fetch(host: &str) -> Result<(String, Vec<u8>), String> {
    let addrs: Vec<SocketAddr> = tokio::net::lookup_host((host, 443)).await.map_err(|e| e.to_string())?.collect();
    // Every address must be public, and the connection goes to the ones checked.
    if addrs.is_empty() || addrs.iter().any(|addr| !is_public(addr.ip())) {
        return Err("Not a public site".into());
    }
    let client = lorca_tls::client_builder()
        .redirect(reqwest::redirect::Policy::none())
        .timeout(Duration::from_secs(8))
        .user_agent("Lorca")
        .resolve_to_addrs(host, &addrs)
        .build()
        .map_err(|e| e.to_string())?;
    let base = reqwest::Url::parse(&format!("https://{host}/")).map_err(|e| e.to_string())?;
    if let Ok(icon) = fetch_icon(&client, host, base.join("/favicon.ico").map_err(|e| e.to_string())?).await {
        return Ok(icon);
    }
    let (_, page) = get(&client, host, base.clone(), MAX_PAGE_BYTES).await?;
    let page = String::from_utf8_lossy(&page);
    for link in icon_links(&page).into_iter().take(3) {
        let Ok(url) = base.join(&link) else { continue };
        if let Ok(icon) = fetch_icon(&client, host, url).await {
            return Ok(icon);
        }
    }
    Err("No icon".into())
}

async fn fetch_icon(client: &reqwest::Client, host: &str, url: reqwest::Url) -> Result<(String, Vec<u8>), String> {
    let (_, bytes) = get(client, host, url, MAX_ICON_BYTES).await?;
    let mime = sniff(&bytes).ok_or("Not an image")?;
    Ok((mime.into(), bytes))
}

/// Whether `url` is on the site whose addresses were checked: HTTPS to `host` on 443, where the
/// client's resolver pins it. An icon link or a redirect elsewhere is never requested.
fn on_site(url: &reqwest::Url, host: &str) -> bool {
    url.scheme() == "https" && url.host_str() == Some(host) && url.port_or_known_default() == Some(443)
}

/// A GET that follows redirects within the same HTTPS host, up to `limit` bytes of body. Every URL
/// is checked before it is requested, the first one too, since a page names its icons.
async fn get(client: &reqwest::Client, host: &str, mut url: reqwest::Url, limit: usize) -> Result<(u16, Vec<u8>), String> {
    for _ in 0..=MAX_REDIRECTS {
        if !on_site(&url, host) {
            return Err("Another site".into());
        }
        let response = client.get(url.clone()).send().await.map_err(|e| lorca_tls::describe(&e))?;
        if response.status().is_redirection() {
            let location = response.headers().get(LOCATION).and_then(|value| value.to_str().ok()).ok_or("Redirect without a place")?;
            url = url.join(location).map_err(|e| e.to_string())?;
            continue;
        }
        let mut response = response.error_for_status().map_err(|e| e.to_string())?;
        let status = response.status().as_u16();
        let mut bytes = Vec::new();
        while let Some(chunk) = response.chunk().await.map_err(|e| e.to_string())? {
            if bytes.len() + chunk.len() > limit {
                return Err("Too large".into());
            }
            bytes.extend_from_slice(&chunk);
        }
        return Ok((status, bytes));
    }
    Err("Too many redirects".into())
}

fn cache_name(host: &str) -> String {
    let digest = Sha256::digest(host.as_bytes());
    format!("{}.icon", data_encoding::HEXLOWER.encode(&digest[..16]))
}

/// What the cache holds for a site that is still fresh: `Some(Some(icon))`, `Some(None)` for a
/// site without one, and `None` for a site not looked up lately.
fn read_cache(path: &Path) -> Option<Option<(String, Vec<u8>)>> {
    let modified = std::fs::metadata(path).ok()?.modified().ok()?;
    let age = SystemTime::now().duration_since(modified).unwrap_or_default();
    let bytes = std::fs::read(path).ok()?;
    if bytes.is_empty() {
        return (age < MISSING_TTL).then_some(None);
    }
    if age >= FOUND_TTL {
        return None;
    }
    let split = bytes.iter().position(|b| *b == b'\n')?;
    let mime = String::from_utf8(bytes[..split].to_vec()).ok()?;
    Some(Some((mime, bytes[split + 1..].to_vec())))
}

fn write_cache(path: &Path, icon: Option<&(String, Vec<u8>)>) {
    let Some(dir) = path.parent() else { return };
    if std::fs::create_dir_all(dir).is_err() {
        return;
    }
    let bytes = match icon {
        Some((mime, data)) => [mime.as_bytes(), b"\n", data].concat(),
        None => Vec::new(),
    };
    let _ = std::fs::write(path, bytes);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_public_site_names_are_looked_up() {
        assert_eq!(valid_host(" WWW.Example.com. ").as_deref(), Some("www.example.com"));
        for host in ["localhost", "127.0.0.1", "10.0.0.1", "a_b.example.com", "-a.example.com", "exa mple.com", "", "http://example.com", "[::1]"] {
            assert!(valid_host(host).is_none(), "{host}");
        }
    }

    #[test]
    fn addresses_on_a_local_network_are_not_public() {
        for ip in ["127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1"] {
            assert!(!is_public(ip.parse().unwrap()), "{ip}");
        }
        for ip in ["1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"] {
            assert!(is_public(ip.parse().unwrap()), "{ip}");
        }
    }

    #[test]
    fn icon_links_and_redirects_stay_on_the_checked_site() {
        let base = reqwest::Url::parse("https://example.com/").unwrap();
        for link in ["/favicon.ico", "icons/a.png", "https://example.com/a.png", "https://example.com:443/a.png"] {
            assert!(on_site(&base.join(link).unwrap(), "example.com"), "{link}");
        }
        for link in ["https://localhost/a.png", "//intranet.example/a.png", "https://10.0.0.1/a.png", "http://example.com/a.png", "https://example.com:8443/a.png", "https://cdn.example.com/a.png"] {
            assert!(!on_site(&base.join(link).unwrap(), "example.com"), "{link}");
        }
    }

    #[test]
    fn only_raster_images_pass_for_icons() {
        assert_eq!(sniff(b"\x89PNG\r\n\x1a\n...."), Some("image/png"));
        assert_eq!(sniff(&[0, 0, 1, 0, 1, 0]), Some("image/x-icon"));
        assert_eq!(sniff(b"RIFF\x00\x00\x00\x00WEBPVP8 "), Some("image/webp"));
        assert_eq!(sniff(b"<svg xmlns=\"http://www.w3.org/2000/svg\"><script/></svg>"), None);
        assert_eq!(sniff(b"<!doctype html>"), None);
    }

    #[test]
    fn a_page_names_its_icons() {
        let page = r#"<head><link rel="stylesheet" href="/a.css"><LINK REL='Shortcut Icon' HREF='/i.ico'>
            <link href="/touch.png" rel="apple-touch-icon" sizes="180x180"><link rel="icon"></head>"#;
        assert_eq!(icon_links(page), ["/i.ico", "/touch.png"]);
    }

    #[test]
    fn the_cache_keeps_an_icon_or_its_absence() {
        let dir = std::env::temp_dir().join(format!("lorca-favicon-{}", uuid::Uuid::new_v4()));
        let found = dir.join(cache_name("a.example.com"));
        let missing = dir.join(cache_name("b.example.com"));
        assert!(read_cache(&found).is_none());
        write_cache(&found, Some(&("image/png".into(), b"\x89PNG\n\x00".to_vec())));
        write_cache(&missing, None);
        assert_eq!(read_cache(&found), Some(Some(("image/png".into(), b"\x89PNG\n\x00".to_vec()))));
        assert_eq!(read_cache(&missing), Some(None));
        let _ = std::fs::remove_dir_all(dir);
    }
}
