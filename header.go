// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import (
	"strings"

	nethttp "github.com/go-ruby-net-http/net-http"
)

// Header is the parsed request-header model produced by ParseHeader: a
// downcased field name maps to its ordered list of values, like the Hash
// WEBrick::HTTPUtils.parse_header returns (each value an element of a
// SplitHeader/CookieHeader array). The multi-value store is the shared HTTP/1.1
// header codec from go-ruby-net-http (nethttp.Header: downcased keys, ordered,
// multi-value, the same Net::HTTPHeader port WEBrick's parse_header builds);
// WEBrick's cookie-specific "; " join is layered on top in Get.
type Header struct {
	multi *nethttp.Header // request-side multi-value store (reused codec)

	// single holds the response-side single-value-per-field model
	// (HTTPResponse @header is a plain Hash); a given Header instance is only
	// ever one kind (request multi or response single), so the two never mix.
	single map[string]string
	order  []string // single-value field order (response side)
}

func newHeader() *Header {
	return &Header{multi: nethttp.NewHeader()}
}

func (h *Header) add(field, value string) {
	// AddField appends to the field's ordered value list, reusing net-http's
	// codec. The value is CR/LF-free (ParseHeader split it on '\n' and trimmed),
	// so AddField's validation never trips.
	_ = h.multi.AddField(field, value)
}

// Get returns the joined value list for the downcased field, with the join
// separator WEBrick uses: "; " for the cookie header (CookieHeader#join), ", "
// for all others (SplitHeader#join). It returns ok=false (and "") when the
// field is absent, matching HTTPRequest#[] returning nil for an empty list.
func (h *Header) Get(field string) (string, bool) {
	dk := strings.ToLower(field)
	if dk == "cookie" {
		// CookieHeader joins with "; " rather than net-http's ", ".
		vals := h.multi.GetFields(dk)
		if len(vals) == 0 {
			return "", false
		}
		return strings.Join(vals, "; "), true
	}
	return h.multi.Get(dk)
}

// Values returns a copy of the raw value list for the downcased field, or nil
// (the reused net-http GetFields).
func (h *Header) Values(field string) []string {
	return h.multi.GetFields(field)
}

// Has reports whether the downcased field is present (net-http Key?).
func (h *Header) Has(field string) bool {
	return h.multi.Key(field)
}

// Each iterates fields in insertion order, calling fn with the downcased name
// and the joined value (nil-equivalent fields are skipped — HTTPRequest#each
// yields nil but callers treat it as absent).
func (h *Header) Each(fn func(name, value string)) {
	// net-http's EachHeader yields downcased name + ", "-joined value in
	// insertion order; for the cookie field WEBrick joins with "; ", so route
	// through Get to apply that.
	h.multi.EachHeader(func(name, _ string) {
		v, _ := h.Get(name)
		fn(name, v)
	})
}

// ParseHeader is the Go port of WEBrick::HTTPUtils.parse_header: it parses a raw
// header block (each line CRLF-terminated) into a Header, folding obs-fold
// continuation lines (leading space/tab) into the prior field, downcasing field
// names, and trimming leading/trailing whitespace from each value. It returns a
// *Status (BadRequest) on a malformed line, matching MRI's raise. The folded,
// trimmed values are then loaded into the shared net-http header codec.
func ParseHeader(raw string) (*Header, *Status) {
	// Accumulate the parsed values locally so the obs-fold continuation can
	// mutate the last value in place, then load the trimmed result into the
	// reused net-http header store (which is otherwise append-only).
	var order []string
	values := map[string][]string{}
	var field string
	haveField := false

	for _, line := range eachLine(raw) {
		name, value, kind := classifyHeaderLine(line)
		switch kind {
		case headerField:
			field = name
			haveField = true
			if _, ok := values[field]; !ok {
				order = append(order, field)
			}
			values[field] = append(values[field], value)
		case headerFold:
			if !haveField {
				return nil, StatusBadRequest("bad header '" + line + "'.")
			}
			vals := values[field]
			vals[len(vals)-1] = vals[len(vals)-1] + " " + value
			values[field] = vals
		default:
			return nil, StatusBadRequest("bad header '" + line + "'.")
		}
	}

	h := newHeader()
	for _, dk := range order {
		for _, v := range values[dk] {
			h.add(dk, strings.Trim(v, " \t"))
		}
	}
	return h, nil
}

type headerLineKind int

const (
	headerBad headerLineKind = iota
	headerField
	headerFold
)

// classifyHeaderLine matches a raw header line (with its trailing CRLF) against
// the two parse_header patterns:
//
//	/^([A-Za-z0-9!#$%&'*+\-.^_`|~]+):([^\r\n\0]*?)\r\n\z/  (a field)
//	/^[ \t]+([^\r\n\0]*?)\r\n/                              (a continuation)
func classifyHeaderLine(line string) (name, value string, kind headerLineKind) {
	if !strings.HasSuffix(line, CRLF) {
		return "", "", headerBad
	}
	body := line[:len(line)-2]
	if strings.ContainsAny(body, "\r\n\x00") {
		return "", "", headerBad
	}
	if len(body) > 0 && (body[0] == ' ' || body[0] == '\t') {
		// continuation: strip leading [ \t]+
		return "", strings.TrimLeft(body, " \t"), headerFold
	}
	colon := strings.IndexByte(body, ':')
	if colon <= 0 {
		return "", "", headerBad
	}
	name = body[:colon]
	if !validFieldName(name) {
		return "", "", headerBad
	}
	return strings.ToLower(name), body[colon+1:], headerField
}

// validFieldName checks the token class [A-Za-z0-9!#$%&'*+\-.^_`|~]+.
func validFieldName(s string) bool {
	// s is never empty here: classifyHeaderLine only calls validFieldName when
	// the colon index is >= 1, so body[:colon] has at least one byte.
	for i := 0; i < len(s); i++ {
		if !fieldNameByte(s[i]) {
			return false
		}
	}
	return true
}

func fieldNameByte(b byte) bool {
	switch {
	case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		return true
	}
	switch b {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

// eachLine splits raw into lines, each including its terminating "\n"
// (String#each_line behaviour). A trailing fragment without a newline is
// returned as its own line.
func eachLine(raw string) []string {
	var out []string
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\n' {
			out = append(out, raw[start:i+1])
			start = i + 1
		}
	}
	if start < len(raw) {
		out = append(out, raw[start:])
	}
	return out
}

// QueryItem is one entry of a parsed query string: a name and its value (the
// FormData string in MRI). Repeated keys chain their values in Next, mirroring
// FormData#append_data.
type QueryItem struct {
	Name  string
	Value string
	Next  *QueryItem
}

// List returns all values for this query item, including chained duplicates
// (FormData#list / #to_ary).
func (q *QueryItem) List() []string {
	var out []string
	for it := q; it != nil; it = it.Next {
		out = append(out, it.Value)
	}
	return out
}

// Query is the parsed query map: a key maps to its (chained) QueryItem. Like
// MRI's Hash it preserves first-insertion order via Order.
type Query struct {
	items map[string]*QueryItem
	Order []string
}

// Get returns the first value for key (Hash#[] of a FormData, whose String
// identity is its first value), and ok=false when absent.
func (q *Query) Get(key string) (string, bool) {
	it, ok := q.items[key]
	if !ok {
		return "", false
	}
	return it.Value, true
}

// Item returns the QueryItem for key (with its full duplicate chain), or nil.
func (q *Query) Item(key string) *QueryItem { return q.items[key] }

// Len reports the number of distinct keys.
func (q *Query) Len() int { return len(q.Order) }

// ParseQuery is the Go port of WEBrick::HTTPUtils.parse_query: it splits str on
// '&' or ';', form-unescapes each key and value, and builds the query map with
// repeated keys chained. An empty element is skipped; a key with no '=' has an
// empty value.
func ParseQuery(str string) *Query {
	q := &Query{items: map[string]*QueryItem{}}
	if str == "" {
		return q
	}
	for _, x := range splitAmpSemi(str) {
		// splitAmpSemi already drops empty elements (the `next if x.empty?`
		// guard in parse_query), so x is always non-empty here.
		key, val := splitKeyVal(x)
		key = UnescapeForm(key)
		val = UnescapeForm(val)
		item := &QueryItem{Name: key, Value: val}
		if existing, ok := q.items[key]; ok {
			// append_data: walk to the chain's tail.
			tail := existing
			for tail.Next != nil {
				tail = tail.Next
			}
			tail.Next = item
			continue
		}
		q.items[key] = item
		q.Order = append(q.Order, key)
	}
	return q
}

// splitAmpSemi splits on the /[&;]/ character class.
func splitAmpSemi(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '&' || r == ';' })
}

// splitKeyVal splits "k=v" on the first '=' (split(/=/, 2)); a value-less key
// has the empty string value (val.to_s of nil).
func splitKeyVal(x string) (key, val string) {
	i := strings.IndexByte(x, '=')
	if i < 0 {
		return x, ""
	}
	return x[:i], x[i+1:]
}
