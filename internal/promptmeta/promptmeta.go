// Package promptmeta parses YAML frontmatter out of prompt markdown files and
// returns the body. Only the body is ever injected (DECISIONS.md §9).
package promptmeta

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Meta holds the supported frontmatter keys (docs/storage/prompt-store.md).
type Meta struct {
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
	Mode        string   `yaml:"mode"`
	Enter       *bool    `yaml:"enter"`
	Key         *string  `yaml:"key"`
	KeyDeclared bool     `yaml:"-"`
}

// Parsed is the result of splitting a prompt file into metadata and body.
type Parsed struct {
	Meta Meta
	Body string
}

// Parse reads prompt-file bytes and returns frontmatter metadata plus body.
func Parse(content []byte) (Parsed, error) {
	normalized := trimUTF8BOM(content)

	metaBytes, body, ok, err := splitFrontmatter(normalized)
	if err != nil {
		return Parsed{}, err
	}
	if !ok {
		return Parsed{Body: string(normalized)}, nil
	}

	var meta Meta
	if err := yaml.Unmarshal(metaBytes, &meta); err != nil {
		return Parsed{}, fmt.Errorf("promptmeta: parse frontmatter: %w", err)
	}

	return Parsed{
		Meta: meta,
		Body: string(body),
	}, nil
}

func trimUTF8BOM(content []byte) []byte {
	if len(content) >= utf8.UTFMax && bytes.Equal(content[:3], []byte{0xEF, 0xBB, 0xBF}) {
		return content[3:]
	}
	return content
}

func (m *Meta) UnmarshalYAML(value *yaml.Node) error {
	type metaAlias Meta

	var alias metaAlias
	if err := value.Decode(&alias); err != nil {
		return err
	}

	*m = Meta(alias)
	m.KeyDeclared = hasTopLevelKey(value, "key")
	return nil
}

func splitFrontmatter(content []byte) (meta []byte, body []byte, ok bool, err error) {
	line, next := nextLine(content, 0)
	if !bytes.Equal(line, []byte("---")) {
		return nil, content, false, nil
	}

	metaStart := next
	for offset := metaStart; offset <= len(content); {
		line, next = nextLine(content, offset)
		if bytes.Equal(line, []byte("---")) {
			candidate := content[metaStart:offset]
			if !looksLikeYAMLFrontmatter(candidate) {
				return nil, content, false, nil
			}
			return candidate, trimSingleLeadingLineBreak(content[next:]), true, nil
		}
		if next == len(content) {
			break
		}
		offset = next
	}

	return nil, content, false, nil
}

func nextLine(content []byte, start int) ([]byte, int) {
	if start >= len(content) {
		return nil, start
	}

	if idx := bytes.IndexByte(content[start:], '\n'); idx >= 0 {
		end := start + idx
		line := content[start:end]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		return line, end + 1
	}

	line := content[start:]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line, len(content)
}

func hasTopLevelKey(node *yaml.Node, key string) bool {
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return false
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

func looksLikeYAMLFrontmatter(content []byte) bool {
	for offset := 0; offset <= len(content); {
		line, next := nextLine(content, offset)
		trimmed := bytes.TrimSpace(line)
		switch {
		case len(trimmed) == 0:
		case trimmed[0] == '#':
		default:
			return bytes.IndexByte(trimmed, ':') > 0 && trimmed[0] != '-'
		}

		if next == len(content) {
			break
		}
		offset = next
	}

	return true
}

func trimSingleLeadingLineBreak(body []byte) []byte {
	switch {
	case bytes.HasPrefix(body, []byte("\r\n")):
		return body[2:]
	case bytes.HasPrefix(body, []byte("\n")):
		return body[1:]
	default:
		return body
	}
}
