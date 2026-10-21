// A file the system's web view draws in the file viewer: an HTML page, a PDF (iOS: WKWebView draws
// one; Android's WebView has none), or a recording it plays. A page is loaded from its text under
// an origin the app does not trust (`PREVIEW_ORIGIN`): it runs its scripts but reads no file, keeps
// no cookies, and has no bridge to the app, which injects nothing and hears no messages. It stays
// where it is: a web or mail link the user follows opens in the browser, and every other
// navigation is cancelled.

import * as WebBrowser from "expo-web-browser";
import { useState } from "react";
import { ActivityIndicator, Linking, Platform, StyleSheet, View } from "react-native";
import { WebView, type WebViewNavigation } from "react-native-webview";
import { PREVIEW_ORIGIN, previewNavigation } from "../core/files";
import { usePalette } from "./theme";

export type PageSource = { html: string } | { pdf: string } | { media: string; video: boolean };

/// Whether this phone's web view draws a kind of file.
export function pageShows(kind: "html" | "pdf" | "media"): boolean {
  return kind !== "pdf" || Platform.OS === "ios";
}

/// The page that plays a recording: the file, alone, on the viewer's ground.
function mediaPage(uri: string, video: boolean, ground: string): string {
  const src = uri.replace(/"/g, "%22");
  const element = video ? `<video src="${src}" controls playsinline autoplay></video>` : `<audio src="${src}" controls autoplay></audio>`;
  return `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><style>
html,body{margin:0;height:100%;background:${ground};display:flex;align-items:center;justify-content:center}
video{width:100%;max-height:100%}audio{width:90%}
</style></head><body>${element}</body></html>`;
}

export function PageView({ source }: { source: PageSource }) {
  const p = usePalette();
  const [loading, setLoading] = useState(true);
  const ground = p.dark ? "#000000" : "#f2f2f7";
  let allowed = PREVIEW_ORIGIN;
  let webSource: { html: string; baseUrl: string } | { uri: string };
  let readsFiles = false;
  if ("html" in source) {
    webSource = { html: source.html, baseUrl: PREVIEW_ORIGIN };
  } else if ("pdf" in source) {
    allowed = source.pdf;
    webSource = { uri: source.pdf };
    readsFiles = true;
  } else if (Platform.OS === "ios") {
    // WKWebView plays a recording it is pointed at.
    allowed = source.media;
    webSource = { uri: source.media };
    readsFiles = true;
  } else {
    // Android's WebView plays one in a page that may read files, under the file's own folder.
    allowed = source.media.slice(0, source.media.lastIndexOf("/") + 1);
    webSource = { html: mediaPage(source.media, source.video, ground), baseUrl: allowed };
    readsFiles = true;
  }

  const decide = (request: WebViewNavigation & { isTopFrame?: boolean }) => {
    if (readsFiles && request.url.startsWith(allowed)) return true;
    // iOS says whether a navigation came from a tap; Android asks only for the top frame's.
    const clicked = Platform.OS === "ios" ? request.navigationType === "click" : request.isTopFrame !== false;
    const verdict = previewNavigation(request.url, allowed, clicked);
    if (verdict === "open") {
      if (request.url.startsWith("mailto:")) void Linking.openURL(request.url);
      else void WebBrowser.openBrowserAsync(request.url);
    }
    return verdict === "load";
  };

  return (
    <View style={[styles.fill, { backgroundColor: ground }]}>
      <WebView
        source={webSource}
        originWhitelist={["https://*", "file://*", "about:*", "mailto:*"]}
        onShouldStartLoadWithRequest={decide}
        onLoadEnd={() => setLoading(false)}
        javaScriptEnabled
        incognito
        domStorageEnabled={false}
        allowFileAccess={readsFiles && Platform.OS === "android"}
        allowFileAccessFromFileURLs={false}
        allowUniversalAccessFromFileURLs={false}
        allowingReadAccessToURL={readsFiles && Platform.OS === "ios" ? allowed.slice(0, allowed.lastIndexOf("/") + 1) : undefined}
        mixedContentMode="never"
        setSupportMultipleWindows={false}
        allowsInlineMediaPlayback
        mediaPlaybackRequiresUserAction={false}
        allowsLinkPreview={false}
        contentInsetAdjustmentBehavior="automatic"
        style={[styles.fill, { backgroundColor: ground }]}
      />
      {loading ? (
        <View style={styles.spinner} pointerEvents="none">
          <ActivityIndicator />
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  spinner: { position: "absolute", top: 0, left: 0, right: 0, bottom: 0, alignItems: "center", justifyContent: "center" },
});
