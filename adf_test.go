package main

import (
	"strings"
	"testing"
)

func TestMarkdownToADF(t *testing.T) {
	markdown := "# Plan\n\nA paragraph with **bold**, *italic*, `code`, and a [link](https://example.com).\n\n- first\n- second\n\n1. one\n2. two\n\n> quoted line\n\n```go\nfmt.Println(\"hi\")\n```"
	doc := markdownToADF(markdown)
	content, ok := doc["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content blocks: %#v", doc)
	}
	kinds := []string{}
	for _, block := range content {
		m := block.(map[string]any)
		kinds = append(kinds, m["type"].(string))
	}
	want := []string{"heading", "paragraph", "bulletList", "orderedList", "blockquote", "codeBlock"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("block types = %v, want %v", kinds, want)
	}

	paragraph := content[1].(map[string]any)["content"].([]any)
	seen := map[string]bool{}
	for _, node := range paragraph {
		m := node.(map[string]any)
		if marks, ok := m["marks"].([]any); ok {
			for _, mark := range marks {
				seen[mark.(map[string]any)["type"].(string)] = true
			}
		}
	}
	for _, mark := range []string{"strong", "em", "code", "link"} {
		if !seen[mark] {
			t.Errorf("missing inline mark %q in %#v", mark, paragraph)
		}
	}

	code := content[5].(map[string]any)
	attrs := code["attrs"].(map[string]any)
	if attrs["language"] != "go" {
		t.Fatalf("code language = %v", attrs["language"])
	}
}

func TestADFRoundTrip(t *testing.T) {
	markdown := "# Title\n\n- alpha\n- beta\n\nText with **bold** and a [site](https://example.com)."
	rendered := adfToMarkdown(markdownToADF(markdown))
	for _, want := range []string{"# Title", "- alpha", "- beta", "**bold**", "[site](https://example.com)"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("round trip missing %q:\n%s", want, rendered)
		}
	}
}

func TestADFToMarkdownForms(t *testing.T) {
	if got := adfToMarkdown("plain text"); got != "plain text" {
		t.Fatalf("plain text = %q", got)
	}
	raw := `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"hello "},{"type":"text","text":"world","marks":[{"type":"strong"}]}]}]}`
	if got := adfToMarkdown(raw); got != "hello **world**" {
		t.Fatalf("ADF markdown = %q", got)
	}
	if got := adfToMarkdown(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
}
