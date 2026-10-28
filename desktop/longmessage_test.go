package main

import (
	"strings"
	"testing"
)

func TestFoldTextEstimatesAndKeepsAHead(t *testing.T) {
	if fold := foldText("Hi there.", 640, textMessage, foldedHeight); fold.folded {
		t.Fatalf("a short message folded: %+v", fold)
	}
	cjk := strings.Repeat("这是一段很长的消息，用来测试折叠。", 40)
	if fold := foldText(cjk, 300, textMessage, foldedHeight); !fold.folded {
		t.Fatal("a long Chinese message did not fold")
	}
	paragraphs := strings.Repeat("Paragraph of a long message.\n\n", 120)
	prefix := foldedPrefix(paragraphs)
	if len([]rune(prefix)) > foldedChars {
		t.Fatalf("prefix is %d runes", len([]rune(prefix)))
	}
	if strings.HasSuffix(prefix, "\n") || strings.HasSuffix(prefix, " ") {
		t.Fatalf("prefix keeps trailing space: %q", prefix[len(prefix)-8:])
	}
	code := "Before.\n\n```ts\n" + strings.Repeat("const line = 1;\n", 300)
	if got := openFences(foldedPrefix(code)); got%2 != 0 {
		t.Fatalf("an open fence stayed open: %d fences", got)
	}
}
