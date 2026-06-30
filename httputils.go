// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package webrick is a pure-Go (no cgo) reimplementation of the deterministic,
// interpreter-independent core of Ruby's WEBrick HTTP server: the HTTP request
// parse, the response build, the HTTPStatus table + error pages, the servlet /
// mount dispatch model, and the HTTPUtils helpers. The TCP accept loop, the
// thread-per-connection model and the FileHandler filesystem reads are
// host-side seams (rbgo wires them); everything here is pure compute and is
// validated byte-for-byte against MRI's `webrick` gem.
package webrick

import (
	"sort"
	"strconv"
	"strings"
)

// CR, LF and CRLF mirror the WEBrick module constants.
const (
	CR   = "\r"
	LF   = "\n"
	CRLF = "\r\n"
)

// The character classes WEBrick::HTTPUtils builds its escape regexes from.
const (
	reserved   = ";/?:@&=+$,"
	numChars   = "0123456789"
	lowAlpha   = "abcdefghijklmnopqrstuvwxyz"
	upAlpha    = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	mark       = "-_.!~*'()"
	unreserved = numChars + lowAlpha + upAlpha + mark
	space      = " "
	delims     = "<>#%\""
	unwise     = "{}|\\^[]`"
)

// byteSet is a 256-entry membership table for one of MRI's character classes.
type byteSet [256]bool

func makeSet(members ...string) byteSet {
	var s byteSet
	for _, m := range members {
		for i := 0; i < len(m); i++ {
			s[m[i]] = true
		}
	}
	return s
}

func controlSet() byteSet {
	var s byteSet
	for c := 0; c <= 0x1f; c++ {
		s[c] = true
	}
	s[0x7f] = true
	return s
}

func nonasciiSet() byteSet {
	var s byteSet
	for c := 0x80; c <= 0xff; c++ {
		s[c] = true
	}
	return s
}

func union(sets ...byteSet) byteSet {
	var out byteSet
	for _, s := range sets {
		for i := 0; i < 256; i++ {
			if s[i] {
				out[i] = true
			}
		}
	}
	return out
}

func complement(members string) byteSet {
	var s byteSet
	in := makeSet(members)
	for i := 0; i < 256; i++ {
		s[i] = !in[i]
	}
	return s
}

// The escape sets, matching the UNESCAPED / UNESCAPED_FORM / NONASCII /
// UNESCAPED_PCHAR regexes in httputils.rb.
var (
	control = controlSet()
	nonAsc  = nonasciiSet()

	unescapedSet     = union(control, makeSet(space, delims, unwise), nonAsc)
	unescapedFormSet = union(makeSet(reserved), control, makeSet(delims, unwise), nonAsc)
	unescapedPChar   = complement(unreserved + ":@&=+$,")
)

const escUpperHex = "0123456789ABCDEF"

// escapeBy percent-encodes every byte of s that is in set, with uppercase hex
// (HTTPUtils._escape: the string is treated as raw bytes).
func escapeBy(s string, set byteSet) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if set[c] {
			b.WriteByte('%')
			b.WriteByte(escUpperHex[c>>4])
			b.WriteByte(escUpperHex[c&0x0f])
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// unescapeAll replaces every %XX in s with the corresponding byte
// (HTTPUtils._unescape with the ESCAPED = /%([0-9a-fA-F]{2})/ regex).
func unescapeAll(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '%' && i+2 < len(s) && isHexDigit(s[i+1]) && isHexDigit(s[i+2]) {
			b.WriteByte(hexVal(s[i+1])<<4 | hexVal(s[i+2]))
			i += 3
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func hexVal(b byte) byte {
	switch {
	case b >= '0' && b <= '9':
		return b - '0'
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10
	default:
		return b - 'A' + 10
	}
}

// Escape percent-encodes HTTP reserved and unwise characters in s
// (WEBrick::HTTPUtils.escape).
func Escape(s string) string { return escapeBy(s, unescapedSet) }

// Unescape decodes every %XX in s (WEBrick::HTTPUtils.unescape).
func Unescape(s string) string { return unescapeAll(s) }

// EscapeForm percent-encodes form-reserved characters and maps space to '+'
// (WEBrick::HTTPUtils.escape_form).
func EscapeForm(s string) string {
	return strings.ReplaceAll(escapeBy(s, unescapedFormSet), " ", "+")
}

// UnescapeForm maps '+' to space then decodes %XX (WEBrick::HTTPUtils.unescape_form).
func UnescapeForm(s string) string {
	return unescapeAll(strings.ReplaceAll(s, "+", " "))
}

// EscapePath escapes each "/segment" of s with the pchar set, mirroring
// WEBrick::HTTPUtils.escape_path: the path is split on '/' and each component
// is escaped, so the slashes themselves are preserved. A path with no '/'
// yields "" (the scan matches nothing), exactly like MRI.
func EscapePath(s string) string {
	var b strings.Builder
	// String#scan(%r{/([^/]*)}) — each match starts at a '/'.
	i := 0
	for i < len(s) {
		if s[i] != '/' {
			i++
			continue
		}
		j := i + 1
		for j < len(s) && s[j] != '/' {
			j++
		}
		b.WriteByte('/')
		b.WriteString(escapeBy(s[i+1:j], unescapedPChar))
		i = j
	}
	return b.String()
}

// Escape8bit percent-encodes the non-ASCII bytes of s (WEBrick::HTTPUtils.escape8bit).
func Escape8bit(s string) string { return escapeBy(s, nonAsc) }

// MimeType returns the MIME type for filename from mimeTab, mirroring
// WEBrick::HTTPUtils.mime_type: it tries the ".ext" suffix, then the
// ".ext.lang" double suffix, each lowercased, falling back to
// "application/octet-stream".
func MimeType(filename string, mimeTab map[string]string) string {
	suffix1 := suffixMatch(filename, false)
	suffix2 := suffixMatch(filename, true)
	if suffix1 != "" {
		if t, ok := mimeTab[suffix1]; ok {
			return t
		}
	}
	if suffix2 != "" {
		if t, ok := mimeTab[suffix2]; ok {
			return t
		}
	}
	return "application/octet-stream"
}

// suffixMatch extracts the suffix MimeType keys on. When double is false it
// matches /\.(\w+)$/; when true it matches /\.(\w+)\.[\w\-]+$/ (the language
// or compression suffix variant). The captured \w+ group is lowercased.
func suffixMatch(filename string, double bool) string {
	if !double {
		// last \.(\w+) anchored at end of string
		dot := lastDotBeforeWordRun(filename, len(filename))
		if dot < 0 {
			return ""
		}
		return strings.ToLower(filename[dot+1:])
	}
	// /\.(\w+)\.[\w\-]+$/ — trailing [\w\-]+, then '.', then (\w+), then '.'.
	end := len(filename)
	// trailing run of [\w-]
	k := end
	for k > 0 && isWordOrHyphen(filename[k-1]) {
		k--
	}
	if k == end || k == 0 || filename[k-1] != '.' {
		return ""
	}
	// now the (\w+) just before that dot
	dot2 := k - 1
	w := dot2
	for w > 0 && isWord(filename[w-1]) {
		w--
	}
	if w == dot2 || w == 0 || filename[w-1] != '.' {
		return ""
	}
	return strings.ToLower(filename[w:dot2])
}

// lastDotBeforeWordRun finds the '.' that begins a trailing run of word chars
// ending at end, i.e. the /\.(\w+)$/ anchor. Returns the index of '.', or -1.
func lastDotBeforeWordRun(s string, end int) int {
	w := end
	for w > 0 && isWord(s[w-1]) {
		w--
	}
	if w == end || w == 0 || s[w-1] != '.' {
		return -1
	}
	return w - 1
}

func isWord(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isWordOrHyphen(b byte) bool { return b == '-' || isWord(b) }

// NormalizePath collapses redundant slashes and resolves "." and ".."
// segments, mirroring WEBrick::HTTPUtils.normalize_path. It returns ok=false
// when the path does not start with '/' or escapes above the root (the two
// `raise "abnormal path"` cases).
func NormalizePath(path string) (string, bool) {
	if path == "" || path[0] != '/' {
		return "", false
	}
	ret := path

	// ret.gsub!(%r{/+}, '/')
	ret = collapseSlashes(ret)
	// while ret.sub!(%r'/\.(?:/|\Z)', '/'); end
	for {
		next, changed := subOnce(ret, dotSegment)
		if !changed {
			break
		}
		ret = next
	}
	// while ret.sub!(%r'/(?!\.\./)[^/]+/\.\.(?:/|\Z)', '/'); end
	for {
		next, changed := subOnce(ret, dotDotSegment)
		if !changed {
			break
		}
		ret = next
	}
	// raise if %r{/\.\.(/|\Z)} =~ ret
	if hasParentRef(ret) {
		return "", false
	}
	return ret, true
}

func collapseSlashes(s string) string {
	var b strings.Builder
	prevSlash := false
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			if prevSlash {
				continue
			}
			prevSlash = true
		} else {
			prevSlash = false
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// dotSegment finds the first "/.", either "/./", or "/." at end of string, and
// returns the [start,end) span to replace with "/". MRI: %r'/\.(?:/|\Z)'.
func dotSegment(s string) (start, end int, ok bool) {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '/' && s[i+1] == '.' {
			if i+2 == len(s) {
				return i, i + 2, true
			}
			if s[i+2] == '/' {
				// The (?:/|\Z) consumes the trailing '/' into the match; the
				// replacement '/' restores a single slash.
				return i, i + 3, true
			}
		}
	}
	return 0, 0, false
}

// dotDotSegment finds the first "/seg/.." (seg != "..") then "/" or end, and
// returns the span to replace with "/". MRI: %r'/(?!\.\./)[^/]+/\.\.(?:/|\Z)'.
func dotDotSegment(s string) (start, end int, ok bool) {
	for i := 0; i < len(s); i++ {
		if s[i] != '/' {
			continue
		}
		// [^/]+ starting at i+1
		j := i + 1
		segStart := j
		for j < len(s) && s[j] != '/' {
			j++
		}
		if j == segStart {
			continue // empty segment
		}
		seg := s[segStart:j]
		if seg == ".." {
			continue // negative lookahead (?!\.\./)
		}
		// require "/.." then ("/" or end). The (?:/|\Z) consumes a trailing '/'
		// so the replacement '/' leaves a single slash.
		if j+2 < len(s) && s[j] == '/' && s[j+1] == '.' && s[j+2] == '.' {
			after := j + 3
			if after == len(s) {
				return i, after, true
			}
			if s[after] == '/' {
				return i, after + 1, true
			}
		}
	}
	return 0, 0, false
}

// subOnce applies the first match of matcher to s, replacing the matched span
// with "/", and reports whether it matched (String#sub! semantics).
func subOnce(s string, matcher func(string) (int, int, bool)) (string, bool) {
	start, end, ok := matcher(s)
	if !ok {
		return s, false
	}
	return s[:start] + "/" + s[end:], true
}

// hasParentRef reports whether s still contains a "/.." followed by "/" or end
// (the final %r{/\.\.(/|\Z)} guard).
func hasParentRef(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '/' && s[i+1] == '.' && s[i+2] == '.' {
			if i+3 == len(s) || s[i+3] == '/' {
				return true
			}
		}
	}
	return false
}

// Dequote removes surrounding quotes and backslash escapes from str
// (WEBrick::HTTPUtils.dequote).
func Dequote(str string) string {
	ret := str
	if len(str) >= 2 && str[0] == '"' && str[len(str)-1] == '"' {
		ret = str[1 : len(str)-1]
	}
	// ret.gsub!(/\\(.)/, "\\1")
	var b strings.Builder
	for i := 0; i < len(ret); i++ {
		if ret[i] == '\\' && i+1 < len(ret) {
			b.WriteByte(ret[i+1])
			i++
			continue
		}
		b.WriteByte(ret[i])
	}
	return b.String()
}

// Quote wraps str in double quotes, mirroring WEBrick::HTTPUtils.quote exactly
// — including its quirk: the replacement string `"\\\1"` is the two bytes
// backslash + 0x01 (the `\1` is a string escape, not a regex backreference), so
// every '"' or '\\' in str is replaced by "\\\x01" (the original char is
// dropped). This reproduces MRI byte-for-byte.
func Quote(str string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(str); i++ {
		if str[i] == '\\' || str[i] == '"' {
			b.WriteByte('\\')
			b.WriteByte(0x01)
			continue
		}
		b.WriteByte(str[i])
	}
	b.WriteByte('"')
	return b.String()
}

// SplitHeaderValue splits a header value into its comma-separated elements,
// honouring quoted strings (so a comma inside "..." does not split), mirroring
// WEBrick::HTTPUtils.split_header_value's scan
// %r'\G((?:"(?:\\.|[^"])+?"|[^",]++)+)(?:,[ \t]*|\Z)'.
func SplitHeaderValue(str string) []string {
	var out []string
	i := 0
	for i < len(str) {
		var elem strings.Builder
		matchedAny := false
		for i < len(str) && str[i] != ',' {
			if str[i] == '"' {
				// quoted run: "(?:\\.|[^"])+?"
				elem.WriteByte('"')
				i++
				for i < len(str) {
					if str[i] == '\\' && i+1 < len(str) {
						elem.WriteByte(str[i])
						elem.WriteByte(str[i+1])
						i += 2
						continue
					}
					if str[i] == '"' {
						elem.WriteByte('"')
						i++
						break
					}
					elem.WriteByte(str[i])
					i++
				}
				matchedAny = true
			} else {
				elem.WriteByte(str[i])
				i++
				matchedAny = true
			}
		}
		if matchedAny {
			out = append(out, elem.String())
		}
		if i < len(str) && str[i] == ',' {
			i++
			// ,[ \t]*
			for i < len(str) && (str[i] == ' ' || str[i] == '\t') {
				i++
			}
		}
	}
	return out
}

// ParseQValues parses a q-value list (an Accept-style header) into the values
// ordered by descending q, mirroring WEBrick::HTTPUtils.parse_qvalues. A nil
// (empty) value yields an empty slice.
func ParseQValues(value string) []string {
	if value == "" {
		return []string{}
	}
	type pair struct {
		val string
		q   float64
	}
	var tmp []pair
	for _, part := range splitCommaWS(value) {
		val, q, ok := matchQValue(part)
		if !ok {
			continue
		}
		tmp = append(tmp, pair{val, q})
	}
	sort.SliceStable(tmp, func(i, j int) bool { return tmp[i].q > tmp[j].q })
	out := make([]string, 0, len(tmp))
	for _, p := range tmp {
		out = append(out, p.val)
	}
	return out
}

// splitCommaWS splits on /,[ \t]*/ (the parse_qvalues separator).
func splitCommaWS(s string) []string {
	var out []string
	i := 0
	start := 0
	for i < len(s) {
		if s[i] == ',' {
			out = append(out, s[start:i])
			i++
			for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
			start = i
			continue
		}
		i++
	}
	out = append(out, s[start:])
	return out
}

// matchQValue applies %r{^([^ \t,]+?)(?:;[ \t]*q=(\d+(?:\.\d+)?))?$} to part.
func matchQValue(part string) (val string, q float64, ok bool) {
	// find ";...q=" suffix
	semi := strings.IndexByte(part, ';')
	head := part
	q = 1.0
	if semi >= 0 {
		head = part[:semi]
		tail := part[semi+1:]
		// [ \t]*q=
		t := strings.TrimLeft(tail, " \t")
		if !strings.HasPrefix(t, "q=") {
			return "", 0, false
		}
		num := t[2:]
		if !validQNumber(num) {
			return "", 0, false
		}
		// validQNumber guarantees \d+(\.\d+)? so ParseFloat cannot fail.
		q, _ = strconv.ParseFloat(num, 64)
	}
	// val = [^ \t,]+? — head must be non-empty and contain no space/tab/comma
	if head == "" || strings.ContainsAny(head, " \t,") {
		return "", 0, false
	}
	return head, q, true
}

// validQNumber checks \d+(?:\.\d+)?.
func validQNumber(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return false
	}
	if i == len(s) {
		return true
	}
	if s[i] != '.' {
		return false
	}
	i++
	if i == len(s) {
		return false
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ParseRangeHeader parses a "bytes=..." Range header into byte ranges, mirroring
// WEBrick::HTTPUtils.parse_range_header. Each Range has First/Last; a missing
// bound is -1 (open-ended) following the Ruby `..-1` convention, and a
// suffix-length range "-N" is First=-N, Last=-1. It returns ok=false when the
// header is not a bytes= range, and a nil slice with ok=false on a bad spec.
func ParseRangeHeader(ranges string) ([]ByteRange, bool) {
	const prefix = "bytes="
	if !strings.HasPrefix(ranges, "bytes=") {
		// MRI requires /^bytes=/; no match returns nil (here ok=false).
		return nil, false
	}
	spec := ranges[len(prefix):]
	var out []ByteRange
	for _, rangeSpec := range SplitHeaderValue(spec) {
		r, ok := parseOneRange(rangeSpec)
		if !ok {
			return nil, false
		}
		out = append(out, r)
	}
	return out, true
}

// ByteRange is one element of a parsed Range header. First/Last follow MRI's
// integer-range convention: Last == -1 means "to the end"; a First < 0 with
// Last == -1 is a suffix length ("-N").
type ByteRange struct {
	First int
	Last  int
}

func parseOneRange(spec string) (ByteRange, bool) {
	// /^(\d+)-(\d+)/
	if i := strings.IndexByte(spec, '-'); i > 0 {
		a := spec[:i]
		b := spec[i+1:]
		if allDigits(a) {
			if allDigits(b) {
				first, _ := strconv.Atoi(a)
				last, _ := strconv.Atoi(b)
				return ByteRange{first, last}, true
			}
			// /^(\d+)-/
			first, _ := strconv.Atoi(a)
			return ByteRange{first, -1}, true
		}
	}
	// /^-(\d+)/
	if strings.HasPrefix(spec, "-") && allDigits(spec[1:]) && len(spec) > 1 {
		n, _ := strconv.Atoi(spec[1:])
		return ByteRange{-n, -1}, true
	}
	return ByteRange{}, false
}
