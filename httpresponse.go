// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import (
	"strconv"
	"strings"
)

// Response is the Go port of WEBrick::HTTPResponse: the status, the header map,
// the body, and the cookies a servlet fills in. Its Bytes method runs MRI's
// setup_header + send_header + send_body and returns the exact response byte
// stream — the payload of the host's write seam (the host writes these bytes to
// the socket).
type Response struct {
	config *Config

	HTTPVersion  HTTPVersion
	Status       int
	ReasonPhrase string

	header *Header // downcased field -> single value (HTTPResponse @header)

	Body    []byte
	Cookies []*Cookie

	RequestMethod      string
	RequestHTTPVersion HTTPVersion
	requestURISet      bool
	requestURIHost     string
	requestURIPort     int

	Filename  string
	chunked   bool
	keepAlive bool
}

// NewResponse creates a response with WEBrick's defaults (status 200, the
// server's HTTP version, keep-alive on), matching HTTPResponse#initialize.
func NewResponse(config *Config) *Response {
	if config == nil {
		config = DefaultConfig()
	}
	return &Response{
		config:             config,
		HTTPVersion:        config.HTTPVersion,
		Status:             200,
		header:             newHeader(),
		keepAlive:          true,
		RequestHTTPVersion: config.HTTPVersion,
	}
}

// SetRequest wires the request method, HTTP version and request-URI host/port
// into the response, mirroring the assignments HTTPServer#run makes before
// servicing (res.request_method = ..., res.request_uri = ...). The request URI
// host/port feed set_error's default address; pass them from the parsed
// Request (req.Host()/req.Port()).
func (r *Response) SetRequest(req *Request) {
	r.RequestMethod = req.RequestMethod
	r.RequestHTTPVersion = req.HTTPVersion
	r.keepAlive = req.KeepAlive()
	r.requestURISet = true
	r.requestURIHost = req.Host()
	r.requestURIPort = req.Port()
}

// Get returns the response header value for field (HTTPResponse#[]).
func (r *Response) Get(field string) (string, bool) {
	return r.header.getSingle(field)
}

// Set sets the response header field to value (HTTPResponse#[]=). Setting
// Transfer-Encoding to "chunked" toggles chunked mode, exactly as MRI's []=.
func (r *Response) Set(field, value string) {
	if strings.EqualFold(field, "transfer-encoding") {
		r.chunked = strings.EqualFold(value, "chunked")
	}
	r.header.setSingle(field, value)
}

// Delete removes a response header field.
func (r *Response) Delete(field string) { r.header.deleteField(field) }

// SetStatus sets the status code and updates the reason phrase
// (HTTPResponse#status=).
func (r *Response) SetStatus(status int) {
	r.Status = status
	r.ReasonPhrase = ReasonPhrase(status)
}

// SetContentType sets the Content-Type header (HTTPResponse#content_type=).
func (r *Response) SetContentType(typ string) { r.Set("content-type", typ) }

// SetContentLength sets the Content-Length header (HTTPResponse#content_length=).
func (r *Response) SetContentLength(n int) { r.Set("content-length", strconv.Itoa(n)) }

// SetChunked enables or disables chunked transfer encoding (HTTPResponse#chunked=).
func (r *Response) SetChunked(v bool) { r.chunked = v }

// Chunked reports whether the body will be chunked (HTTPResponse#chunked?).
func (r *Response) Chunked() bool { return r.chunked }

// KeepAlive reports the keep-alive state (HTTPResponse#keep_alive?).
func (r *Response) KeepAlive() bool { return r.keepAlive }

// StatusLine returns the response status line, mirroring HTTPResponse#status_line:
// "HTTP/<ver> <status> <reason>".rstrip + CRLF (so a nil reason yields no
// trailing space).
func (r *Response) StatusLine() string {
	line := "HTTP/" + r.HTTPVersion.String() + " " + strconv.Itoa(r.Status) + " " + r.ReasonPhrase
	return strings.TrimRight(line, " \t\r\n\v\f") + CRLF
}

// SetRedirect sets a redirect to url with the given redirect Status (the
// HTTPStatus::Redirect subclass), mirroring HTTPResponse#set_redirect: it sets
// the HTML body, the Location header, and returns the status to "raise".
func (r *Response) SetRedirect(status *Status, url string) *Status {
	r.Body = []byte("<HTML><A HREF=\"" + url + "\">" + url + "</A>.</HTML>\n")
	r.Set("location", url)
	return status
}

// SetError fills the response from an error, mirroring HTTPResponse#set_error:
// an HTTPStatus::Status sets that status (clearing keep-alive for an error
// code); anything else becomes 500. It then sets the ISO-8859-1 content type and
// generates the default error-page HTML.
func (r *Response) SetError(err error) {
	if st, ok := err.(*Status); ok && st != nil && st.Code != 0 {
		if IsError(st.Code) {
			r.keepAlive = false
		}
		r.SetStatus(st.Code)
	} else {
		r.keepAlive = false
		r.SetStatus(500)
	}
	r.Set("content-type", "text/html; charset=ISO-8859-1")
	host, port := r.errorAddress()
	r.errorBody(err, host, port)
}

func (r *Response) errorAddress() (string, int) {
	if r.requestURISet && r.requestURIHost != "" {
		return r.requestURIHost, r.requestURIPort
	}
	return r.config.ServerName, r.config.Port
}

// errorBody builds the default error page, mirroring HTTPResponse#error_body.
func (r *Response) errorBody(err error, host string, port int) {
	var b strings.Builder
	b.WriteString("<!DOCTYPE HTML PUBLIC \"-//W3C//DTD HTML 4.0//EN\">\n")
	b.WriteString("<HTML>\n")
	b.WriteString("  <HEAD><TITLE>" + HTMLEscape(r.ReasonPhrase) + "</TITLE></HEAD>\n")
	b.WriteString("  <BODY>\n")
	b.WriteString("    <H1>" + HTMLEscape(r.ReasonPhrase) + "</H1>\n")
	b.WriteString("    " + HTMLEscape(errMessage(err)) + "\n")
	b.WriteString("    <HR>\n")
	b.WriteString("    <ADDRESS>\n")
	b.WriteString("     " + HTMLEscape(r.config.ServerSoftware) + " at\n")
	b.WriteString("     " + host + ":" + strconv.Itoa(port) + "\n")
	b.WriteString("    </ADDRESS>\n")
	b.WriteString("  </BODY>\n")
	b.WriteString("</HTML>\n")
	r.Body = []byte(b.String())
}

func errMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Bytes runs setup_header + send_header + send_body and returns the response
// byte stream (HTTPResponse#send_response's payload). It returns an error only
// if a header value contains CR/LF (HTTPResponse::InvalidHeader). HEAD requests
// suppress the body, exactly as send_body does.
func (r *Response) Bytes() ([]byte, error) {
	r.setupHeader()
	head, err := r.sendHeader()
	if err != nil {
		return nil, err
	}
	body := r.sendBody()
	return append(head, body...), nil
}

// InvalidHeaderError corresponds to HTTPResponse::InvalidHeader.
type InvalidHeaderError struct{ Field, Value string }

func (e *InvalidHeaderError) Error() string {
	return "invalid header: " + e.Field + ": " + e.Value
}

// setupHeader is the Go port of HTTPResponse#setup_header: it seeds Server, the
// keep-alive/Connection decision, and the message-length framing
// (Content-Length vs chunked vs delete-for-bodyless). The Date header is
// non-deterministic (Time.now.httpdate) so it is left to the host to add; every
// other framing header matches MRI byte-for-byte.
func (r *Response) setupHeader() {
	if r.ReasonPhrase == "" {
		r.ReasonPhrase = ReasonPhrase(r.Status)
	}
	if !r.header.has("server") {
		r.header.setSingle("server", r.config.ServerSoftware)
	}

	// HTTP/0.9 + HTTP/1.0 feature downgrades.
	if r.RequestHTTPVersion.Less(HTTPVersion{1, 0}) {
		r.HTTPVersion = HTTPVersion{0, 9}
		r.keepAlive = false
	}
	if r.RequestHTTPVersion.Less(HTTPVersion{1, 1}) {
		if r.chunked {
			r.chunked = false
		}
	}

	// Message length (RFC2616 4.4).
	ctype, _ := r.header.getSingle("content-type")
	switch {
	case r.Status == 304 || r.Status == 204 || IsInfo(r.Status):
		r.header.deleteField("content-length")
		r.Body = nil
	case r.chunked:
		r.header.setSingle("transfer-encoding", "chunked")
		r.header.deleteField("content-length")
	case strings.HasPrefix(ctype, "multipart/byteranges"):
		r.header.deleteField("content-length")
	case !r.header.has("content-length"):
		r.header.setSingle("content-length", strconv.Itoa(len(r.Body)))
	}

	// Keep-Alive / Connection.
	conn, _ := r.header.getSingle("connection")
	switch {
	case conn == "close":
		r.keepAlive = false
	case r.keepAlive:
		if r.chunked || r.header.has("content-length") || r.Status == 304 || r.Status == 204 || IsInfo(r.Status) {
			r.header.setSingle("connection", "Keep-Alive")
		} else {
			r.header.setSingle("connection", "close")
			r.keepAlive = false
		}
	default:
		r.header.setSingle("connection", "close")
	}

	// Location merging is request-URI relative-resolution, which needs the full
	// request URI; the absolute-URL common case is preserved as-is here.
}

// sendHeader ports HTTPResponse#send_header: the status line, then each header
// with the WEBrick header-name capitalisation, then Set-Cookie lines, then a
// blank line. HTTP/0.9 emits no header block.
func (r *Response) sendHeader() ([]byte, error) {
	if r.HTTPVersion.Major == 0 {
		return []byte{}, nil
	}
	var b strings.Builder
	b.WriteString(r.StatusLine())
	for _, field := range r.header.order {
		value := r.header.single[field]
		if strings.ContainsAny(value, "\r\n") {
			return nil, &InvalidHeaderError{Field: field, Value: value}
		}
		b.WriteString(capitalizeHeaderName(field))
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteString(CRLF)
	}
	for _, c := range r.Cookies {
		cs := c.String()
		if strings.ContainsAny(cs, "\r\n") {
			return nil, &InvalidHeaderError{Field: "set-cookie", Value: cs}
		}
		b.WriteString("Set-Cookie: ")
		b.WriteString(cs)
		b.WriteString(CRLF)
	}
	b.WriteString(CRLF)
	return []byte(b.String()), nil
}

// capitalizeHeaderName ports send_header's key.gsub(/\bwww|^te$|\b\w/){ $&.upcase }:
// uppercase a leading "www" run, the whole "te", and the first letter of every
// word (split on non-word boundaries, '-' being the separator).
func capitalizeHeaderName(name string) string {
	if name == "te" {
		return "TE"
	}
	out := []byte(name)
	// \bwww -> WWW
	for _, seg := range wordSpans(name) {
		if name[seg[0]:seg[1]] == "www" {
			for i := seg[0]; i < seg[1]; i++ {
				out[i] = upcaseByte(out[i])
			}
		}
	}
	// \b\w -> uppercase first char of each word
	prevWord := false
	for i := 0; i < len(out); i++ {
		isW := isWord(out[i])
		if isW && !prevWord {
			out[i] = upcaseByte(out[i])
		}
		prevWord = isW
	}
	return string(out)
}

// wordSpans returns [start,end) spans of maximal \w+ runs in s.
func wordSpans(s string) [][2]int {
	var spans [][2]int
	i := 0
	for i < len(s) {
		if !isWord(s[i]) {
			i++
			continue
		}
		j := i
		for j < len(s) && isWord(s[j]) {
			j++
		}
		spans = append(spans, [2]int{i, j})
		i = j
	}
	return spans
}

func upcaseByte(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 32
	}
	return b
}

// sendBody ports HTTPResponse#send_body_string: HEAD sends nothing; chunked
// frames the body into a single chunk (the body is one in-memory string here)
// plus the terminating "0\r\n\r\n"; otherwise the raw body bytes.
func (r *Response) sendBody() []byte {
	if r.RequestMethod == "HEAD" {
		return []byte{}
	}
	if r.chunked {
		var b strings.Builder
		if len(r.Body) > 0 {
			b.WriteString(strconv.FormatInt(int64(len(r.Body)), 16))
			b.WriteString(CRLF)
			b.Write(r.Body)
			b.WriteString(CRLF)
		}
		b.WriteString("0")
		b.WriteString(CRLF)
		b.WriteString(CRLF)
		return []byte(b.String())
	}
	if len(r.Body) > 0 {
		out := make([]byte, len(r.Body))
		copy(out, r.Body)
		return out
	}
	return []byte{}
}

// Header field helpers on Header: the response stores a single value per field
// (a Hash, not a SplitHeader), so add a single-value layer alongside the parse
// multi-value store.

func (h *Header) ensureSingle() {
	if h.single == nil {
		h.single = map[string]string{}
	}
}

func (h *Header) setSingle(field, value string) {
	h.ensureSingle()
	dk := strings.ToLower(field)
	if _, ok := h.single[dk]; !ok {
		h.order = append(h.order, dk)
	}
	h.single[dk] = value
}

func (h *Header) getSingle(field string) (string, bool) {
	dk := strings.ToLower(field)
	v, ok := h.single[dk]
	return v, ok
}

func (h *Header) has(field string) bool {
	_, ok := h.single[strings.ToLower(field)]
	return ok
}

func (h *Header) deleteField(field string) {
	dk := strings.ToLower(field)
	if _, ok := h.single[dk]; ok {
		delete(h.single, dk)
		for i, k := range h.order {
			if k == dk {
				h.order = append(h.order[:i], h.order[i+1:]...)
				break
			}
		}
	}
}
