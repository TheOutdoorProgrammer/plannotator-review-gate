package main

import "strings"

// A comment locator, not a parser: it only lets the gate tell "you reworded a
// comment" from "you changed code". It errs toward finding FEWER comments — a
// miss costs one extra review, a false positive lets code through unreviewed.

// span is a comment's byte range in the source: [start, end).
type span struct {
	start, end int
}

// delimiter is an open/close token pair. For line comments close is "" (runs to
// end of line); for same-delimiter strings/docstrings open == close.
type delimiter struct {
	open, close string
}

// langSyntax describes how one language delimits comments and strings. Strings
// are tracked only so a comment token inside a literal ("http://x") is not
// mistaken for a comment. strEsc marks string kinds where a backslash escapes
// the closing quote (Go/JS/Python quotes; NOT raw/backtick).
type langSyntax struct {
	line   []string
	block  []delimiter
	doc    []delimiter // triple-quote docstrings (Python); treated as comments
	strEsc []delimiter // strings with backslash escaping
	strRaw []delimiter // strings without escaping (raw/backtick/single-quote sh)
}

// syntaxByExt maps a lowercased extension to its syntax. An absent extension is
// never treated as comment-only, so unknown file types always get reviewed.
// C/Rust/Java share the C-family entry.
var syntaxByExt = func() map[string]langSyntax {
	cFamily := langSyntax{
		line:   []string{"//"},
		block:  []delimiter{{"/*", "*/"}},
		strEsc: []delimiter{{`"`, `"`}, {"'", "'"}},
	}
	goSyntax := langSyntax{
		line:   []string{"//"},
		block:  []delimiter{{"/*", "*/"}},
		strEsc: []delimiter{{`"`, `"`}, {"'", "'"}},
		strRaw: []delimiter{{"`", "`"}},
	}
	jsSyntax := goSyntax
	py := langSyntax{
		line:   []string{"#"},
		doc:    []delimiter{{`"""`, `"""`}, {"'''", "'''"}},
		strEsc: []delimiter{{`"`, `"`}, {"'", "'"}},
	}
	sh := langSyntax{
		line:   []string{"#"},
		strEsc: []delimiter{{`"`, `"`}},
		strRaw: []delimiter{{"'", "'"}},
	}
	lua := langSyntax{
		line:   []string{"--"},
		block:  []delimiter{{"--[[", "]]"}},
		strEsc: []delimiter{{`"`, `"`}, {"'", "'"}},
		strRaw: []delimiter{{"[[", "]]"}},
	}
	hcl := langSyntax{
		line:   []string{"#", "//"},
		block:  []delimiter{{"/*", "*/"}},
		strEsc: []delimiter{{`"`, `"`}},
	}
	yaml := langSyntax{
		line:   []string{"#"},
		strEsc: []delimiter{{`"`, `"`}},
		strRaw: []delimiter{{"'", "'"}},
	}
	return map[string]langSyntax{
		".go":   goSyntax,
		".py":   py,
		".pyi":  py,
		".js":   jsSyntax,
		".jsx":  jsSyntax,
		".ts":   jsSyntax,
		".tsx":  jsSyntax,
		".mjs":  jsSyntax,
		".cjs":  jsSyntax,
		".lua":  lua,
		".sh":   sh,
		".bash": sh,
		".zsh":  sh,
		".tf":   hcl,
		".tofu": hcl,
		".yaml": yaml,
		".yml":  yaml,
		".c":    cFamily,
		".h":    cFamily,
		".cpp":  cFamily,
		".cc":   cFamily,
		".hpp":  cFamily,
		".rs":   cFamily,
		".java": cFamily,
	}
}()

// scanComments returns the byte span of every comment and docstring in src.
// Content inside string literals is skipped, so a comment token in a string is
// not mistaken for a comment.
func scanComments(src string, syn langSyntax) []span {
	var out []span
	i := 0
	for i < len(src) {
		if d, ok := matchDelim(src, i, syn.doc); ok {
			end := blockEnd(src, i, d)
			out = append(out, span{i, end})
			i = end
			continue
		}
		if d, ok := matchDelim(src, i, syn.block); ok {
			end := blockEnd(src, i, d)
			out = append(out, span{i, end})
			i = end
			continue
		}
		if matchToken(src, i, syn.line) {
			end := lineEnd(src, i)
			out = append(out, span{i, end})
			i = end
			continue
		}
		if d, ok := matchDelim(src, i, syn.strEsc); ok {
			i = skipString(src, i, d, true)
			continue
		}
		if d, ok := matchDelim(src, i, syn.strRaw); ok {
			i = skipString(src, i, d, false)
			continue
		}
		i++
	}
	return out
}

func matchToken(src string, i int, tokens []string) bool {
	for _, t := range tokens {
		if strings.HasPrefix(src[i:], t) {
			return true
		}
	}
	return false
}

func matchDelim(src string, i int, delims []delimiter) (delimiter, bool) {
	for _, d := range delims {
		if strings.HasPrefix(src[i:], d.open) {
			return d, true
		}
	}
	return delimiter{}, false
}

// lineEnd returns the index of the newline ending this line (or end of source).
// The newline itself is left outside the span so stripping preserves it.
func lineEnd(src string, i int) int {
	if end := strings.IndexByte(src[i:], '\n'); end >= 0 {
		return i + end
	}
	return len(src)
}

// blockEnd returns the index just past the closing delimiter. An unterminated
// block runs to end of source.
func blockEnd(src string, i int, d delimiter) int {
	rest := src[i+len(d.open):]
	closeAt := strings.Index(rest, d.close)
	if closeAt < 0 {
		return len(src)
	}
	return i + len(d.open) + closeAt + len(d.close)
}

// skipString advances past a string literal so comment tokens inside it are
// ignored. When esc is true a backslash escapes the closing quote. An
// unterminated string runs to end of source.
func skipString(src string, i int, d delimiter, esc bool) int {
	j := i + len(d.open)
	for j < len(src) {
		if esc && src[j] == '\\' {
			j += 2
			continue
		}
		if strings.HasPrefix(src[j:], d.close) {
			return j + len(d.close)
		}
		j++
	}
	return len(src)
}
