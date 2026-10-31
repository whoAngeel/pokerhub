package llm

import (
	"fmt"
	"strings"
)

// extractBalancedJSON returns the first balanced top-level `{...}` object
// found in s. It is used by the "prompt" structured mode, where the model
// isn't forced into a response_format and may wrap its JSON in prose or a
// markdown code fence.
//
// It understands JSON string literals (including backslash escapes), so
// braces that appear inside a quoted string don't confuse the brace counter.
func extractBalancedJSON(s string) (string, error) {
	s = stripCodeFences(s)

	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", fmt.Errorf("no JSON object found in output")
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1], nil
			}
		}
	}
	return "", fmt.Errorf("unbalanced JSON object in output")
}

// stripCodeFences removes a leading/trailing markdown code fence (``` or
// ```json) around s, if present, so extractBalancedJSON can find the object
// inside it.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return s
	}
	s = s[nl+1:]
	if idx := strings.LastIndex(s, "```"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}
