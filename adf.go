package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var orderedItemPattern = regexp.MustCompile(`^\s*\d+\.\s+(.*)$`)

func markdownToADF(text string) map[string]any {
	blocks := []any{}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			language := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			i++
			code := []string{}
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				code = append(code, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			attrs := map[string]any{}
			if language != "" {
				attrs["language"] = language
			}
			block := map[string]any{"type": "codeBlock"}
			if len(attrs) > 0 {
				block["attrs"] = attrs
			}
			content := adfInlineText(strings.Join(code, "\n"))
			if len(content) > 0 {
				block["content"] = content
			}
			blocks = append(blocks, block)
			continue
		}

		if trimmed == "" {
			i++
			continue
		}

		if level, heading, ok := headingLine(trimmed); ok {
			blocks = append(blocks, map[string]any{
				"type":    "heading",
				"attrs":   map[string]any{"level": level},
				"content": adfInlineText(heading),
			})
			i++
			continue
		}

		if isRule(trimmed) {
			blocks = append(blocks, map[string]any{"type": "rule"})
			i++
			continue
		}

		if strings.HasPrefix(trimmed, "> ") || trimmed == ">" {
			quote := []string{}
			for i < len(lines) {
				current := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(current, ">") {
					break
				}
				current = strings.TrimSpace(strings.TrimPrefix(current, ">"))
				quote = append(quote, current)
				i++
			}
			blocks = append(blocks, map[string]any{
				"type":    "blockquote",
				"content": paragraphNodes(strings.Join(quote, "\n")),
			})
			continue
		}

		if bulletLine, ok := bulletText(trimmed); ok {
			items := []any{}
			for i < len(lines) {
				current := strings.TrimSpace(lines[i])
				text, ok := bulletText(current)
				if !ok {
					break
				}
				items = append(items, listItem(text))
				i++
			}
			_ = bulletLine
			blocks = append(blocks, map[string]any{"type": "bulletList", "content": items})
			continue
		}

		if match := orderedItemPattern.FindStringSubmatch(trimmed); match != nil {
			items := []any{}
			for i < len(lines) {
				current := strings.TrimSpace(lines[i])
				m := orderedItemPattern.FindStringSubmatch(current)
				if m == nil {
					break
				}
				items = append(items, listItem(m[1]))
				i++
			}
			blocks = append(blocks, map[string]any{"type": "orderedList", "content": items})
			continue
		}

		paragraph := []string{trimmed}
		i++
		for i < len(lines) {
			next := strings.TrimSpace(lines[i])
			if next == "" || strings.HasPrefix(next, "```") || strings.HasPrefix(next, "> ") ||
				strings.HasPrefix(next, "- ") || strings.HasPrefix(next, "* ") || strings.HasPrefix(next, "+ ") ||
				orderedItemPattern.MatchString(next) {
				break
			}
			if _, _, ok := headingLine(next); ok {
				break
			}
			if isRule(next) {
				break
			}
			paragraph = append(paragraph, next)
			i++
		}
		nodes := paragraphNodes(strings.Join(paragraph, "\n"))
		blocks = append(blocks, map[string]any{"type": "paragraph", "content": nodes})
	}
	if len(blocks) == 0 {
		blocks = []any{map[string]any{"type": "paragraph", "content": []any{}}}
	}
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": blocks,
	}
}

func headingLine(line string) (int, string, bool) {
	count := 0
	for count < len(line) && line[count] == '#' {
		count++
	}
	if count == 0 || count > 6 {
		return 0, "", false
	}
	if len(line) <= count || line[count] != ' ' {
		return 0, "", false
	}
	return count, strings.TrimSpace(line[count+1:]), true
}

func isRule(line string) bool {
	if len(strings.Trim(line, "-")) == 0 && len(line) >= 3 {
		return true
	}
	if len(strings.Trim(line, "*")) == 0 && len(line) >= 3 {
		return true
	}
	return false
}

func bulletText(line string) (string, bool) {
	for _, prefix := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix)), true
		}
	}
	return "", false
}

func listItem(text string) map[string]any {
	return map[string]any{
		"type":    "listItem",
		"content": []any{map[string]any{"type": "paragraph", "content": adfInlineText(text)}},
	}
}

func paragraphNodes(text string) []any {
	lines := strings.Split(text, "\n")
	nodes := []any{}
	for idx, line := range lines {
		if idx > 0 {
			nodes = append(nodes, map[string]any{"type": "hardBreak"})
		}
		nodes = append(nodes, adfInlineText(line)...)
	}
	return nodes
}

func adfInlineText(text string) []any {
	nodes := []any{}
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			nodes = append(nodes, map[string]any{"type": "text", "text": plain.String()})
			plain.Reset()
		}
	}
	for i := 0; i < len(text); {
		if strings.HasPrefix(text[i:], "**") {
			if end := strings.Index(text[i+2:], "**"); end >= 0 {
				flush()
				nodes = append(nodes, markedText(text[i+2:i+2+end], "strong"))
				i += 2 + end + 2
				continue
			}
		}
		if strings.HasPrefix(text[i:], "__") {
			if end := strings.Index(text[i+2:], "__"); end >= 0 {
				flush()
				nodes = append(nodes, markedText(text[i+2:i+2+end], "strong"))
				i += 2 + end + 2
				continue
			}
		}
		if text[i] == '`' {
			if end := strings.Index(text[i+1:], "`"); end >= 0 {
				flush()
				nodes = append(nodes, markedText(text[i+1:i+1+end], "code"))
				i += 1 + end + 1
				continue
			}
		}
		if text[i] == '*' || text[i] == '_' {
			marker := string(text[i])
			if end := strings.Index(text[i+1:], marker); end >= 0 && end > 0 {
				flush()
				nodes = append(nodes, markedText(text[i+1:i+1+end], "em"))
				i += 1 + end + 1
				continue
			}
		}
		if text[i] == '[' {
			if closeLabel := strings.Index(text[i+1:], "]("); closeLabel >= 0 {
				labelEnd := i + 1 + closeLabel
				if closeURL := strings.Index(text[labelEnd+2:], ")"); closeURL >= 0 {
					urlEnd := labelEnd + 2 + closeURL
					label := text[i+1 : labelEnd]
					href := text[labelEnd+2 : urlEnd]
					flush()
					node := map[string]any{
						"type": "text",
						"text": label,
						"marks": []any{map[string]any{
							"type":  "link",
							"attrs": map[string]any{"href": href},
						}},
					}
					nodes = append(nodes, node)
					i = urlEnd + 1
					continue
				}
			}
		}
		plain.WriteByte(text[i])
		i++
	}
	flush()
	return nodes
}

func markedText(text, mark string) map[string]any {
	return map[string]any{
		"type":  "text",
		"text":  text,
		"marks": []any{map[string]any{"type": mark}},
	}
}

func adfToMarkdown(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return ""
		}
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var decoded any
			if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
				return adfToMarkdown(decoded)
			}
		}
		return v
	case map[string]any:
		return renderADFNode(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			rendered := adfToMarkdown(item)
			if strings.TrimSpace(rendered) != "" {
				parts = append(parts, rendered)
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return fmt.Sprint(v)
	}
}

func renderADFNode(node map[string]any) string {
	kind, _ := node["type"].(string)
	switch kind {
	case "doc":
		return renderADFChildren(node, "\n\n")
	case "paragraph":
		return renderInlineChildren(node)
	case "heading":
		level := 1
		if attrs, ok := node["attrs"].(map[string]any); ok {
			if n, ok := numberToInt(attrs["level"]); ok && n >= 1 && n <= 6 {
				level = n
			}
		}
		return strings.Repeat("#", level) + " " + strings.TrimSpace(renderInlineChildren(node))
	case "bulletList", "orderedList":
		children := adfContent(node)
		lines := make([]string, 0, len(children))
		for idx, child := range children {
			rendered := strings.TrimSpace(adfToMarkdown(child))
			prefix := "- "
			if kind == "orderedList" {
				prefix = strconv.Itoa(idx+1) + ". "
			}
			lines = append(lines, prefix+strings.ReplaceAll(rendered, "\n", "\n  "))
		}
		return strings.Join(lines, "\n")
	case "listItem":
		return strings.TrimSpace(renderADFChildren(node, "\n"))
	case "codeBlock":
		language := ""
		if attrs, ok := node["attrs"].(map[string]any); ok {
			if lang, ok := attrs["language"].(string); ok {
				language = lang
			}
		}
		return "```" + language + "\n" + renderPlainChildren(node) + "\n```"
	case "blockquote":
		body := renderADFChildren(node, "\n\n")
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			lines[i] = "> " + line
		}
		return strings.Join(lines, "\n")
	case "rule":
		return "---"
	case "hardBreak":
		return "\n"
	case "text":
		text, _ := node["text"].(string)
		return applyMarks(text, node["marks"])
	case "mention":
		if attrs, ok := node["attrs"].(map[string]any); ok {
			if text, ok := attrs["text"].(string); ok && text != "" {
				return "@" + text
			}
		}
		return "@unknown"
	case "inlineCard", "blockCard", "embedCard":
		if attrs, ok := node["attrs"].(map[string]any); ok {
			if url, ok := attrs["url"].(string); ok {
				return url
			}
		}
		return ""
	case "mediaSingle", "mediaGroup", "panel", "expand", "table", "tableRow", "tableCell", "tableHeader":
		return renderADFChildren(node, "\n")
	case "media":
		if attrs, ok := node["attrs"].(map[string]any); ok {
			if alt, ok := attrs["alt"].(string); ok && alt != "" {
				return "[media: " + alt + "]"
			}
		}
		return "[media]"
	default:
		return renderADFChildren(node, "\n")
	}
}

func renderADFChildren(node map[string]any, separator string) string {
	children := adfContent(node)
	parts := make([]string, 0, len(children))
	for _, child := range children {
		rendered := adfToMarkdown(child)
		if strings.TrimSpace(rendered) != "" {
			parts = append(parts, strings.TrimRight(rendered, "\n"))
		}
	}
	return strings.Join(parts, separator)
}

func renderPlainChildren(node map[string]any) string {
	children := adfContent(node)
	var b strings.Builder
	for _, child := range children {
		if m, ok := child.(map[string]any); ok {
			if text, ok := m["text"].(string); ok {
				b.WriteString(text)
				continue
			}
		}
		b.WriteString(adfToMarkdown(child))
	}
	return b.String()
}

func renderInlineChildren(node map[string]any) string {
	children := adfContent(node)
	var b strings.Builder
	for _, child := range children {
		if m, ok := child.(map[string]any); ok {
			if kind, _ := m["type"].(string); kind == "hardBreak" {
				b.WriteString("\n")
				continue
			}
			if kind, _ := m["type"].(string); kind == "text" {
				b.WriteString(adfToMarkdown(m))
				continue
			}
			b.WriteString(adfToMarkdown(m))
			continue
		}
		b.WriteString(adfToMarkdown(child))
	}
	return b.String()
}

func applyMarks(text string, marks any) string {
	list, ok := marks.([]any)
	if !ok {
		return text
	}
	for _, mark := range list {
		m, ok := mark.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := m["type"].(string)
		switch kind {
		case "strong":
			text = "**" + text + "**"
		case "em":
			text = "*" + text + "*"
		case "code":
			text = "`" + text + "`"
		case "strike":
			text = "~~" + text + "~~"
		case "link":
			if attrs, ok := m["attrs"].(map[string]any); ok {
				if href, ok := attrs["href"].(string); ok && href != "" {
					text = "[" + text + "](" + href + ")"
				}
			}
		}
	}
	return text
}

func adfContent(node map[string]any) []any {
	if content, ok := node["content"].([]any); ok {
		return content
	}
	return nil
}

func numberToInt(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}
