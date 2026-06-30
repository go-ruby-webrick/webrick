// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import (
	"strconv"
	"strings"
)

// Request is the Go port of WEBrick::HTTPRequest: the parsed request line,
// header model, decoded body, query and cookies. Unlike MRI it does not read
// from a socket itself — the host hands ParseRequest the complete request byte
// stream (the read seam), and Request exposes the same accessors WEBrick
// servlets use (request_method, path, query, cookies, keep_alive?, []).
type Request struct {
	config *Config

	RequestLine   string
	RequestMethod string
	UnparsedURI   string
	HTTPVersion   HTTPVersion

	Path        string
	ScriptName  string
	PathInfo    string
	QueryString string
	host        string
	port        int

	RawHeader []string
	header    *Header
	Cookies   []*Cookie

	Accept         []string
	AcceptCharset  []string
	AcceptEncoding []string
	AcceptLanguage []string

	Body []byte

	keepAlive bool
	query     *Query

	// parsed reports whether the URI/body machinery ran (false for CONNECT and
	// asterisk-form, matching MRI's early return).
	bodyParsed bool
}

const (
	maxURILength    = 2083
	maxHeaderLength = 112 * 1024
)

// ParseRequest is the Go port of WEBrick::HTTPRequest#parse over a complete
// request byte stream: the request line, the header block, the cookies and
// Accept q-value lists, the request URI (path / host / port / query), the
// keep-alive decision, and the body (Content-Length or chunked). The socket /
// peer addresses are a host seam, so addr-derived host defaulting falls back to
// the config ServerName/Port. It returns a *Status on any malformed input,
// exactly the WEBrick::HTTPStatus error MRI would raise.
func ParseRequest(raw []byte, config *Config) (*Request, *Status) {
	if config == nil {
		config = DefaultConfig()
	}
	r := &Request{config: config, keepAlive: false}

	p := &reqParser{data: raw}

	if st := r.readRequestLine(p); st != nil {
		return nil, st
	}

	if r.HTTPVersion.Major > 0 {
		if st := r.readHeader(p); st != nil {
			return nil, st
		}
		for _, c := range r.header.Values("cookie") {
			r.Cookies = append(r.Cookies, ParseCookies(c)...)
		}
		r.Accept = ParseQValues(r.field("accept"))
		r.AcceptCharset = ParseQValues(r.field("accept-charset"))
		r.AcceptEncoding = ParseQValues(r.field("accept-encoding"))
		r.AcceptLanguage = ParseQValues(r.field("accept-language"))
	} else {
		// HTTP/0.9: no header block, empty header model.
		r.header = newHeader()
	}

	if r.RequestMethod == "CONNECT" || r.UnparsedURI == "*" {
		return r, nil
	}

	if st := r.parseURI(); st != nil {
		return nil, st
	}
	r.computeKeepAlive()

	if st := r.readBody(p); st != nil {
		return nil, st
	}
	r.bodyParsed = true
	return r, nil
}

// field returns the joined header value for name, or "" when absent — the
// HTTPRequest#[] accessor (which returns nil for an empty list; "" here).
func (r *Request) field(name string) string {
	if r.header == nil {
		return ""
	}
	v, ok := r.header.Get(name)
	if !ok {
		return ""
	}
	return v
}

// Header returns a header value with an ok flag (HTTPRequest#[]).
func (r *Request) Header(name string) (string, bool) {
	if r.header == nil {
		return "", false
	}
	return r.header.Get(name)
}

// EachHeader iterates the request headers (HTTPRequest#each).
func (r *Request) EachHeader(fn func(name, value string)) {
	if r.header != nil {
		r.header.Each(fn)
	}
}

// Host returns the request host (HTTPRequest#host).
func (r *Request) Host() string { return r.host }

// Port returns the request port (HTTPRequest#port).
func (r *Request) Port() int { return r.port }

// KeepAlive reports whether this connection should be kept alive
// (HTTPRequest#keep_alive?).
func (r *Request) KeepAlive() bool { return r.keepAlive }

// ContentLength returns the parsed Content-Length (HTTPRequest#content_length).
// It returns ok=false when the header is absent.
func (r *Request) ContentLength() (int, bool) {
	v := r.field("content-length")
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, false
	}
	return n, true
}

// ContentType returns the Content-Type header (HTTPRequest#content_type).
func (r *Request) ContentType() string { return r.field("content-type") }

// Query returns the parsed query (HTTPRequest#query): for GET/HEAD it parses the
// query string; for an x-www-form-urlencoded body it parses the body; for
// multipart it is not modelled (the host handles multipart streaming). The
// result is memoised.
func (r *Request) Query() *Query {
	if r.query != nil {
		return r.query
	}
	switch {
	case r.RequestMethod == "GET" || r.RequestMethod == "HEAD":
		r.query = ParseQuery(r.QueryString)
	case strings.HasPrefix(r.ContentType(), "application/x-www-form-urlencoded"):
		r.query = ParseQuery(string(r.Body))
	default:
		r.query = &Query{items: map[string]*QueryItem{}}
	}
	return r.query
}

func (r *Request) readRequestLine(p *reqParser) *Status {
	line, ok := p.readLine(maxURILength)
	if !ok {
		return &Status{Code: 0, ReasonPhrase: "EOF", Message: "EOFError"}
	}
	r.RequestLine = line
	if len(line) >= maxURILength && !strings.HasSuffix(line, LF) {
		return StatusRequestEntityTooLarge("request URI too large")
	}

	method, uri, ver, ok := matchRequestLine(line)
	if !ok {
		rl := strings.TrimRight(line, "\r\n")
		return StatusBadRequest("bad Request-Line '" + rl + "'.")
	}
	r.RequestMethod = method
	r.UnparsedURI = uri
	if ver == "" {
		ver = "0.9"
	}
	// ver is "0.9" or a token matchRequestLine already validated as \d+.\d+,
	// so ParseHTTPVersion cannot fail here.
	hv, _ := ParseHTTPVersion(ver)
	r.HTTPVersion = hv
	return nil
}

// matchRequestLine ports /^(\S+) (\S++)(?: HTTP\/(\d+\.\d+))?\r\n/m:
// method, a single space, the URI, an optional " HTTP/x.y", then CRLF.
func matchRequestLine(line string) (method, uri, ver string, ok bool) {
	if !strings.HasSuffix(line, CRLF) {
		return "", "", "", false
	}
	body := line[:len(line)-2]
	sp := strings.IndexByte(body, ' ')
	if sp <= 0 {
		return "", "", "", false
	}
	method = body[:sp]
	if strings.ContainsAny(method, " \t\r\n\f\v") {
		return "", "", "", false
	}
	rest := body[sp+1:]
	// \S++ for the URI, then optional " HTTP/(\d+\.\d+)".
	if i := strings.LastIndex(rest, " HTTP/"); i >= 0 {
		uri = rest[:i]
		ver = rest[i+len(" HTTP/"):]
		if !isVersionToken(ver) {
			// " HTTP/" not followed by a version: the whole rest is the URI,
			// but then it would contain a space, which \S++ forbids.
			return "", "", "", false
		}
	} else {
		uri = rest
		ver = ""
	}
	if uri == "" || strings.ContainsAny(uri, " \t\r\n\f\v") {
		return "", "", "", false
	}
	return method, uri, ver, true
}

func isVersionToken(s string) bool {
	dot := strings.IndexByte(s, '.')
	if dot <= 0 || dot == len(s)-1 {
		return false
	}
	return allDigits(s[:dot]) && allDigits(s[dot+1:])
}

func (r *Request) readHeader(p *reqParser) *Status {
	endOfHeaders := false
	requestBytes := len(r.RequestLine)
	for {
		line, ok := p.readLine(4096)
		if !ok {
			break
		}
		if line == CRLF {
			endOfHeaders = true
			break
		}
		requestBytes += len(line)
		if requestBytes > maxHeaderLength {
			return StatusRequestEntityTooLarge("headers too large")
		}
		if strings.ContainsRune(line, 0) {
			return StatusBadRequest("null byte in header")
		}
		r.RawHeader = append(r.RawHeader, line)
	}
	if !endOfHeaders {
		return &Status{Code: 0, ReasonPhrase: "EOF", Message: "EOFError"}
	}
	h, st := ParseHeader(strings.Join(r.RawHeader, ""))
	if st != nil {
		return st
	}
	r.header = h

	cl := h.Values("content-length")
	if len(cl) != 0 {
		if len(cl) > 1 {
			return StatusBadRequest("multiple content-length request headers")
		}
		if !allDigits(cl[0]) {
			return StatusBadRequest("invalid content-length request header")
		}
	}
	return nil
}

func (r *Request) readBody(p *reqParser) *Status {
	if tc := r.field("transfer-encoding"); tc != "" {
		if r.field("content-length") != "" {
			return StatusBadRequest("request with both transfer-encoding and content-length, possible request smuggling")
		}
		if !strings.EqualFold(tc, "chunked") {
			return StatusNotImplemented("Transfer-Encoding: " + tc + ".")
		}
		return r.readChunked(p)
	}
	if v := r.field("content-length"); v != "" {
		n, _ := strconv.Atoi(v)
		data, ok := p.readN(n)
		if !ok {
			return StatusBadRequest("invalid body size.")
		}
		r.Body = data
		return nil
	}
	if r.RequestMethod == "POST" || r.RequestMethod == "PUT" {
		return StatusLengthRequired()
	}
	return nil
}

func (r *Request) readChunked(p *reqParser) *Status {
	var out []byte
	for {
		size, st := r.readChunkSize(p)
		if st != nil {
			return st
		}
		if size == 0 {
			break
		}
		data, ok := p.readN(size)
		if !ok || len(data) != size {
			return StatusBadRequest("bad chunk data size.")
		}
		out = append(out, data...)
		// skip CRLF
		line, ok := p.readLine(4096)
		if !ok || line != CRLF {
			return StatusBadRequest("extra data after chunk '" + line + "'.")
		}
	}
	// trailer + CRLF: read header lines until blank.
	for {
		line, ok := p.readLine(4096)
		if !ok || line == CRLF {
			break
		}
	}
	r.Body = out
	return nil
}

// readChunkSize ports /^([0-9a-fA-F]+)(?:;(\S+(?:=\S+)?))?\r\n$/.
func (r *Request) readChunkSize(p *reqParser) (int, *Status) {
	line, ok := p.readLine(4096)
	if !ok {
		return 0, StatusBadRequest("bad chunk ''.")
	}
	if !strings.HasSuffix(line, CRLF) {
		return 0, StatusBadRequest("bad chunk '" + line + "'.")
	}
	body := line[:len(line)-2]
	hexPart := body
	if semi := strings.IndexByte(body, ';'); semi >= 0 {
		hexPart = body[:semi]
		ext := body[semi+1:]
		if !validChunkExt(ext) {
			return 0, StatusBadRequest("bad chunk '" + line + "'.")
		}
	}
	if hexPart == "" || !isHexRun(hexPart) {
		return 0, StatusBadRequest("bad chunk '" + line + "'.")
	}
	n, err := strconv.ParseInt(hexPart, 16, 64)
	if err != nil {
		return 0, StatusBadRequest("bad chunk '" + line + "'.")
	}
	return int(n), nil
}

// validChunkExt checks \S+(?:=\S+)? — at least one non-space char.
func validChunkExt(ext string) bool {
	if ext == "" {
		return false
	}
	return !strings.ContainsAny(ext, " \t\r\n\f\v")
}

func isHexRun(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isHexDigit(s[i]) {
			return false
		}
	}
	return s != ""
}

func (r *Request) computeKeepAlive() {
	conn := r.field("connection")
	switch {
	case strings.EqualFold(strings.TrimSpace(conn), "close"):
		r.keepAlive = false
	case strings.EqualFold(strings.TrimSpace(conn), "keep-alive"):
		r.keepAlive = true
	case r.HTTPVersion.Less(HTTPVersion{Major: 1, Minor: 1}):
		r.keepAlive = false
	default:
		r.keepAlive = true
	}
}

// reqParser is a forward byte cursor over the raw request (the BufferedIO
// surface MRI's read_line/read use).
type reqParser struct {
	data []byte
	pos  int
}

// readLine returns the next line up to and including '\n', capped at size bytes
// (io.gets(LF, size)). ok=false at EOF with nothing buffered.
func (p *reqParser) readLine(size int) (string, bool) {
	if p.pos >= len(p.data) {
		return "", false
	}
	end := len(p.data)
	limit := p.pos + size
	for i := p.pos; i < end; i++ {
		if p.data[i] == '\n' {
			end = i + 1
			break
		}
	}
	if end > limit {
		end = limit
	}
	line := string(p.data[p.pos:end])
	p.pos = end
	return line, true
}

// readN returns exactly n bytes; ok=false if fewer remain. n is always >= 0
// (a Content-Length digit run or a hex chunk size).
func (p *reqParser) readN(n int) ([]byte, bool) {
	if p.pos+n > len(p.data) {
		return nil, false
	}
	b := make([]byte, n)
	copy(b, p.data[p.pos:p.pos+n])
	p.pos += n
	return b, true
}
