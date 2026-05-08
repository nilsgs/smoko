package parser

import (
	"strings"
)

// TokenKind classifies a lexed line.
type TokenKind int

const (
	TokFeature    TokenKind = iota // Feature:
	TokBackground                  // Background:
	TokScenario                    // Scenario:
	TokImage                       // Image:
	TokGiven                       // Given
	TokWhen                        // When
	TokThen                        // Then
	TokAnd                         // And
	TokBut                         // But
	TokTag                         // @tag
	TokBlock                       // indented continuation line
	TokComment                     // # ...
	TokBlank                       // empty line
)

// Token is a single classified line.
type Token struct {
	Kind    TokenKind
	Payload string // text after the keyword (trimmed); for TokBlock: the raw indented line (trimmed)
	Line    int    // 1-based source line number
	Indent  int    // number of leading spaces (used for block detection)
}

// keywords maps prefix -> TokenKind and the offset past the keyword.
var keywords = []struct {
	prefix string
	kind   TokenKind
}{
	{"Feature:", TokFeature},
	{"Background:", TokBackground},
	{"Scenario:", TokScenario},
	{"Image:", TokImage},
	{"Given ", TokGiven},
	{"Given\t", TokGiven},
	{"When ", TokWhen},
	{"When\t", TokWhen},
	{"Then ", TokThen},
	{"Then\t", TokThen},
	{"And ", TokAnd},
	{"And\t", TokAnd},
	{"But ", TokBut},
	{"But\t", TokBut},
}

// Tokenize converts raw lines into a slice of Tokens.
// It is stateful: after a step whose payload ends with ":", subsequent
// indented lines are classified as TokBlock (not TokComment), so that
// file content like "#!/bin/sh" is not mistaken for a comment.
func Tokenize(lines []string) []Token {
	tokens := make([]Token, 0, len(lines))
	inBlock := false
	blockIndent := 0

	for i, raw := range lines {
		lineNum := i + 1
		indent := countLeadingSpaces(raw)
		trimmed := strings.TrimSpace(raw)

		if trimmed == "" {
			tokens = append(tokens, Token{Kind: TokBlank, Line: lineNum})
			// blank line ends a block
			inBlock = false
			continue
		}

		// If we're inside a multi-line block, treat any indented line as block
		// content (even if it starts with '#').
		if inBlock && indent > blockIndent {
			tokens = append(tokens, Token{Kind: TokBlock, Payload: trimmed, Line: lineNum, Indent: indent})
			continue
		}
		inBlock = false // left the block indentation level

		// Regular comment detection (only outside block context)
		if strings.HasPrefix(trimmed, "#") {
			tokens = append(tokens, Token{Kind: TokComment, Payload: trimmed[1:], Line: lineNum})
			continue
		}
		if strings.HasPrefix(trimmed, "@") {
			tokens = append(tokens, Token{Kind: TokTag, Payload: trimmed, Line: lineNum, Indent: indent})
			continue
		}

		matched := false
		for _, kw := range keywords {
			if strings.HasPrefix(trimmed, kw.prefix) {
				payload := strings.TrimSpace(trimmed[len(kw.prefix):])
				tok := Token{Kind: kw.kind, Payload: payload, Line: lineNum, Indent: indent}
				tokens = append(tokens, tok)
				matched = true
				// If the step payload ends with ':', next indented lines are block content.
				if strings.HasSuffix(payload, ":") || strings.HasSuffix(payload, ": ") {
					inBlock = true
					blockIndent = indent
				}
				break
			}
		}
		if !matched {
			tokens = append(tokens, Token{Kind: TokBlock, Payload: trimmed, Line: lineNum, Indent: indent})
		}
	}
	return tokens
}

func countLeadingSpaces(s string) int {
	n := 0
	for _, c := range s {
		if c == ' ' {
			n++
		} else if c == '\t' {
			n += 4
		} else {
			break
		}
	}
	return n
}
