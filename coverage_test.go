// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import (
	"strings"
	"testing"
)

// TestEdgeBranches exercises the remaining error/edge branches to keep the
// deterministic suite at 100% coverage independent of the MRI oracle.
func TestEdgeBranches(t *testing.T) {
	// HTMLEscape: each replacement, and a string with all of &"<>.
	if HTMLEscape(`a&b"c<d>e`) != "a&amp;b&quot;c&lt;d&gt;e" {
		t.Errorf("HTMLEscape = %q", HTMLEscape(`a&b"c<d>e`))
	}

	// Has on the multi-value request header model.
	r, _ := ParseRequest([]byte("GET / HTTP/1.1\r\nHost: h\r\n\r\n"), nil)
	if !r.header.Has("host") || r.header.Has("nope") {
		t.Error("Header.Has")
	}
	// Values copy of an absent key is nil.
	if r.header.Values("absent") != nil {
		t.Error("Values absent should be nil")
	}

	// MountProc routing.
	srv := NewHTTPServer(nil)
	srv.MountProc("/p", func(req *Request, res *Response) *Status { res.Body = []byte("proc"); return nil })
	req := &Request{RequestMethod: "GET", Path: "/p", UnparsedURI: "/p"}
	res := NewResponse(nil)
	if st := srv.Service(req, res); st != nil || string(res.Body) != "proc" {
		t.Errorf("MountProc = %v %q", st, res.Body)
	}

	// atoiSafe non-digit termination.
	if atoiSafe("12x3") != 12 || atoiSafe("") != 0 {
		t.Errorf("atoiSafe = %d", atoiSafe("12x3"))
	}

	// A $Version directive before any cookie sets the version for the cookies
	// that follow; the $Path/$Domain/$Port directives apply to the current
	// cookie (the nil-guard mirrors that they only take effect after one exists).
	cs := ParseCookies("$Version=1; a=1; $Path=/p; $Domain=d; $Port=8080")
	if len(cs) != 1 || cs[0].Version != 1 || cs[0].Path != "/p" || cs[0].Domain != "d" || cs[0].Port != "8080" {
		t.Errorf("cookie directives = %#v", cs[0])
	}

	// validChunkExt: empty (no semicolon means hex-only; force a bad ext).
	if _, st := ParseRequest([]byte("POST /u HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n4; bad ext\r\nWiki\r\n0\r\n\r\n"), nil); st == nil {
		t.Error("bad chunk ext should fail")
	}

	// chunk size line without CRLF (truncated).
	if _, st := ParseRequest([]byte("POST /u HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n4"), nil); st == nil {
		t.Error("truncated chunk size should fail")
	}

	// validQNumber edge: empty number after q=, and a bare integer.
	if got := ParseQValues("a;q="); len(got) != 0 {
		t.Errorf("q= empty = %v", got)
	}
	if got := ParseQValues("a;q=1"); strings.Join(got, ",") != "a" {
		t.Errorf("q=1 = %v", got)
	}

	// splitAuthority with bad port + IPv6 without close bracket + IPv6 bad port.
	if _, st := ParseRequest([]byte("GET http://h:notaport/p HTTP/1.1\r\nHost: h\r\n\r\n"), nil); st == nil {
		t.Error("bad authority port should fail")
	}
	if _, st := ParseRequest([]byte("GET http://[::1/p HTTP/1.1\r\nHost: h\r\n\r\n"), nil); st == nil {
		t.Error("unterminated ipv6 should fail")
	}
	if _, st := ParseRequest([]byte("GET http://[::1]x/p HTTP/1.1\r\nHost: h\r\n\r\n"), nil); st == nil {
		t.Error("ipv6 bad trailing should fail")
	}

	// matchRequestLine: a request line with no space.
	if _, st := ParseRequest([]byte("GET\r\n\r\n"), nil); st == nil {
		t.Error("no-space request line should fail")
	}
	// request line " HTTP/" not followed by a version.
	if _, st := ParseRequest([]byte("GET /p HTTP/x.y\r\n\r\n"), nil); st == nil {
		t.Error("non-version HTTP/ should fail")
	}

	// absolute URI with empty path -> defaults to "/".
	r2, st := ParseRequest([]byte("GET http://h:8080 HTTP/1.1\r\nHost: h\r\n\r\n"), nil)
	if st != nil || r2.Path != "/" {
		t.Errorf("absolute no path = %q, %v", r2.Path, st)
	}
}

// TestSetErrorNonStatus covers SetError with a non-Status error (-> 500).
func TestSetErrorNonStatus(t *testing.T) {
	res := NewResponse(nil)
	res.RequestMethod = "GET"
	res.SetError(errString("boom"))
	if res.Status != 500 {
		t.Errorf("non-status error -> %d", res.Status)
	}
	if !strings.Contains(string(res.Body), "boom") {
		t.Errorf("error body = %q", res.Body)
	}
	// SetError with nil error message branch (errMessage nil) via a Status whose
	// Code is 0 (treated as non-status -> 500 path uses the err's Error()).
	res2 := NewResponse(nil)
	res2.RequestMethod = "GET"
	res2.requestURISet = true
	res2.requestURIHost = "h"
	res2.requestURIPort = 9
	res2.SetError(StatusBadRequest("bad"))
	if res2.Status != 400 {
		t.Errorf("bad request status = %d", res2.Status)
	}
}

// TestErrMessageNil covers errMessage(nil).
func TestErrMessageNil(t *testing.T) {
	if errMessage(nil) != "" {
		t.Error("errMessage(nil) should be empty")
	}
}

// TestScanSegmentsLeadingNonSlash covers a path_info with no leading slash.
func TestScanSegmentsLeadingNonSlash(t *testing.T) {
	if len(scanSegments("noslash")) != 0 {
		t.Error("scanSegments with no slash should be empty")
	}
	if len(scanSegments("/a/b")) != 2 {
		t.Error("scanSegments /a/b")
	}
}

// TestParseHeaderFoldFirst covers a continuation line with no preceding field.
func TestParseHeaderFoldFirst(t *testing.T) {
	if _, st := ParseHeader(" leading-space\r\n"); st == nil {
		t.Error("leading continuation should be BadRequest")
	}
	if _, st := ParseHeader("noColonHere\r\n"); st == nil {
		t.Error("no-colon header should be BadRequest")
	}
	if _, st := ParseHeader(":novalue\r\n"); st == nil {
		t.Error("empty field name should be BadRequest")
	}
	// a line not ending in CRLF.
	if _, st := ParseHeader("X: y"); st == nil {
		t.Error("non-CRLF header line should be BadRequest")
	}
	// valid fold continues a value.
	h, st := ParseHeader("X: a\r\n b\r\n")
	if st != nil {
		t.Fatalf("fold: %v", st)
	}
	if v, _ := h.Get("x"); v != "a b" {
		t.Errorf("folded value = %q", v)
	}
}

// TestDotDotEdge covers dotDotSegment where ".." is the segment (lookahead) and
// hasParentRef positive.
func TestDotDotEdge(t *testing.T) {
	// "/../" cannot be removed (the (?!\.\./) lookahead), so it fails.
	if _, ok := NormalizePath("/a/../../b"); ok {
		t.Error("/a/../../b should fail")
	}
	if _, ok := NormalizePath("/.."); ok {
		t.Error("/.. should fail")
	}
}

// TestVersionTokenEdge covers isVersionToken false branches via request lines.
func TestVersionTokenEdge(t *testing.T) {
	// " HTTP/1" (no dot) is not a version token.
	if _, st := ParseRequest([]byte("GET /p HTTP/1\r\n\r\n"), nil); st == nil {
		t.Error("HTTP/1 no dot should fail")
	}
}

// TestMoreEdgeBranches mops up the final defensive/edge branches for 100%.
func TestMoreEdgeBranches(t *testing.T) {
	// HTMLEscape("") empty branch.
	if HTMLEscape("") != "" {
		t.Error("HTMLEscape empty")
	}

	// A bare Request (nil header) exercises the field/Header/Query nil guards.
	bare := &Request{RequestMethod: "GET"}
	if bare.ContentType() != "" {
		t.Error("bare ContentType")
	}
	if _, ok := bare.Header("x"); ok {
		t.Error("bare Header")
	}
	if _, ok := bare.ContentLength(); ok {
		t.Error("bare ContentLength")
	}
	bare.EachHeader(func(k, v string) { t.Error("bare EachHeader should not yield") })
	// GET Query branch on a bare request.
	bare.QueryString = "a=1"
	if v, _ := bare.Query().Get("a"); v != "1" {
		t.Error("bare GET query")
	}

	// ContentLength with a malformed value (non-numeric) -> ok=false. The parse
	// path rejects non-digits, so inject a bad value into the header store.
	r, _ := ParseRequest([]byte("GET / HTTP/1.1\r\nHost: h\r\n\r\n"), nil)
	_ = r.header.multi.AddField("content-length", "xyz")
	if _, ok := r.ContentLength(); ok {
		t.Error("malformed content-length should be ok=false")
	}

	// cookie-header join uses "; ".
	r2, _ := ParseRequest([]byte("GET / HTTP/1.1\r\nCookie: a=1\r\nCookie: b=2\r\n\r\n"), nil)
	if v, _ := r2.header.Get("cookie"); v != "a=1; b=2" {
		t.Errorf("cookie join = %q", v)
	}
	// absent cookie header -> Get cookie branch returns ok=false.
	r2b, _ := ParseRequest([]byte("GET / HTTP/1.1\r\nHost: h\r\n\r\n"), nil)
	if _, ok := r2b.header.Get("cookie"); ok {
		t.Error("absent cookie should be ok=false")
	}

	// header line with a CR inside the body (not the terminator) -> bad.
	if _, st := ParseHeader("X: a\rb\r\n"); st == nil {
		t.Error("CR in header body should be bad")
	}

	// validFieldName empty (a line ":x") already covered; a field name with an
	// invalid char.
	if _, st := ParseHeader("X Y: z\r\n"); st == nil {
		t.Error("space in field name should be bad")
	}

	// dotDotSegment: "/.." at end of a longer path.
	if p, ok := NormalizePath("/a/b/.."); !ok || p != "/a/" {
		t.Errorf("/a/b/.. = %q,%v", p, ok)
	}
	// "/a/.." -> "/" via the end-of-string dot-dot branch.
	if p, ok := NormalizePath("/a/.."); !ok || p != "/" {
		t.Errorf("/a/.. = %q,%v", p, ok)
	}

	// MimeType suffix2-only hit: a name whose only matching suffix is the
	// double form. "name.txt.en" -> suffix1 "en" (miss), suffix2 "txt" (hit).
	if MimeType("name.txt.en", DefaultMimeTypes) != "text/plain" {
		t.Errorf("mime suffix2 = %q", MimeType("name.txt.en", DefaultMimeTypes))
	}

	// schemeEnd: a scheme-like prefix with no "://" is treated as origin-form
	// fail (no leading slash) -> BadRequest.
	if _, st := ParseRequest([]byte("GET mailto:x HTTP/1.1\r\nHost: h\r\n\r\n"), nil); st == nil {
		t.Error("scheme without // should fail as non-origin")
	}

	// splitAuthority IPv6 with empty rest after ']'.
	r3, st := ParseRequest([]byte("GET http://[::1]/p HTTP/1.1\r\nHost: h\r\n\r\n"), nil)
	if st != nil || r3.Host() != "[::1]" || r3.Port() != 80 {
		t.Errorf("ipv6 no port = %q:%d, %v", r3.Host(), r3.Port(), st)
	}

	// parsePort empty (host with trailing colon) in absolute URI.
	r4, st := ParseRequest([]byte("GET http://h:/p HTTP/1.1\r\nHost: h\r\n\r\n"), nil)
	if st != nil || r4.Host() != "h" || r4.Port() != 80 {
		t.Errorf("trailing colon authority = %q:%d, %v", r4.Host(), r4.Port(), st)
	}

	// http (non-https) absolute URL without port -> defaultPortForScheme http=80.
	r5, _ := ParseRequest([]byte("GET http://h/p HTTP/1.1\r\nHost: h\r\n\r\n"), nil)
	if r5.Port() != 80 {
		t.Errorf("http default port = %d", r5.Port())
	}

	// method containing a tab -> matchRequestLine method-whitespace branch.
	if _, st := ParseRequest([]byte("GE\tT / HTTP/1.1\r\n\r\n"), nil); st == nil {
		t.Error("tab in method should fail")
	}

	// setup_header with a preset reason phrase (skip the default) and a preset
	// chunked on an HTTP/1.0 request (downgrade branch).
	res := NewResponse(nil)
	res.ReasonPhrase = "Custom"
	res.RequestHTTPVersion = HTTPVersion{1, 0}
	res.SetStatus(200)
	res.ReasonPhrase = "Custom"
	res.SetChunked(true)
	res.Body = []byte("x")
	if _, err := res.Bytes(); err != nil {
		t.Fatal(err)
	}
	if res.Chunked() {
		t.Error("chunked should be downgraded for HTTP/1.0")
	}

	// response with Connection: close preset.
	res2 := NewResponse(nil)
	res2.SetStatus(200)
	res2.Set("connection", "close")
	res2.Body = []byte("x")
	if _, err := res2.Bytes(); err != nil {
		t.Fatal(err)
	}
	if res2.KeepAlive() {
		t.Error("Connection: close should clear keep-alive")
	}

	// SearchServlet miss (scan matches "" but nothing mounted there).
	srv := NewHTTPServer(nil)
	srv.Mount("/foo", NewProcServlet(func(req *Request, res *Response) *Status { return nil }))
	if _, _, _, ok := srv.SearchServlet("/bar"); ok {
		t.Error("SearchServlet /bar with only /foo mounted should miss")
	}

	// FileHandler: a nondisclosure directory name encountered mid-walk.
	fs := &fakeFS{
		dirs:  map[string]bool{"/r": true, "/r/.htdir": true},
		files: map[string]bool{"/r/.htdir/f": true},
	}
	h := NewFileHandler("/r", fs, DefaultFileHandlerOptions(nil))
	if _, st := h.ResolveFile("/.htdir/f"); st == nil {
		t.Error("nondisclosure directory should be NotFound")
	}

	// globMatch trailing '*' consumption.
	if !globMatch("ab*", "abcdef") {
		t.Error("glob trailing star")
	}
}

// TestFinalBranches mops up the last reachable error/edge branches.
func TestFinalBranches(t *testing.T) {
	// ParseQuery 3+ duplicate keys exercises the chain-tail walk.
	q := ParseQuery("a=1&a=2&a=3")
	if list := q.Item("a").List(); strings.Join(list, ",") != "1,2,3" {
		t.Errorf("triple-dup = %v", list)
	}

	// Request URI exceeding MAX_URI_LENGTH without a trailing LF -> too large.
	longURI := "GET /" + strings.Repeat("a", 2100) + " HTTP/1.1"
	if _, st := ParseRequest([]byte(longURI), nil); st == nil {
		t.Error("over-long request line should be RequestEntityTooLarge")
	}

	// Header block exceeding MAX_HEADER_LENGTH.
	big := "GET / HTTP/1.1\r\n" + "X-Big: " + strings.Repeat("v", 120*1024) + "\r\n\r\n"
	if _, st := ParseRequest([]byte(big), nil); st == nil {
		t.Error("over-long header should be RequestEntityTooLarge")
	}

	// Empty-ext chunk: "4;\r\n" (semicolon then nothing) -> bad chunk.
	if _, st := ParseRequest([]byte("POST /u HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n4;\r\nWiki\r\n0\r\n\r\n"), nil); st == nil {
		t.Error("empty chunk ext should fail")
	}

	// Chunked body whose trailing chunk has no CRLF after the data.
	if _, st := ParseRequest([]byte("POST /u HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nWikiX0\r\n\r\n"), nil); st == nil {
		t.Error("missing chunk CRLF should fail")
	}

	// readN short body: Content-Length larger than the available bytes.
	if _, st := ParseRequest([]byte("POST / HTTP/1.1\r\nContent-Length: 50\r\n\r\nshort"), nil); st == nil {
		t.Error("short readN should fail")
	}

	// Non-origin, non-absolute target with an alpha run and no colon -> the
	// schemeEnd no-colon branch then BadRequest.
	if _, st := ParseRequest([]byte("GET foobar HTTP/1.1\r\nHost: h\r\n\r\n"), nil); st == nil {
		t.Error("bare word target should fail")
	}

	// A request line that is just a method with no CRLF terminator on the line
	// returned (request line w/o version, no CRLF) -> matchRequestLine no-CRLF.
	if _, st := ParseRequest([]byte("GET /p HTTP/1.1"), nil); st == nil {
		t.Error("request line without CRLF should fail")
	}

	// setup_header default reason phrase (status set directly, phrase empty).
	res := NewResponse(nil)
	res.Status = 404
	res.ReasonPhrase = ""
	res.Body = []byte("x")
	out, err := res.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "HTTP/1.1 404 Not Found\r\n") {
		t.Errorf("default reason = %q", out[:30])
	}

	// FileHandler: a file directly under root whose basename is a nondisclosure
	// name reached via the file branch (line 131 checkFilename on a file).
	fs := &fakeFS{
		dirs:  map[string]bool{"/r": true},
		files: map[string]bool{"/r/.htaccess": true},
	}
	h := NewFileHandler("/r", fs, DefaultFileHandlerOptions(nil))
	if _, st := h.ResolveFile("/.htaccess"); st == nil {
		t.Error("nondisclosure file should be NotFound")
	}

	// FileHandler: index file whose name is nondisclosure (line 118). Use a
	// DirectoryIndex of a hidden name to trigger checkFilename on the index.
	opts := DefaultFileHandlerOptions(nil)
	opts.DirectoryIndex = []string{".htindex"}
	fs2 := &fakeFS{
		dirs:  map[string]bool{"/r2": true, "/r2/d": true},
		files: map[string]bool{"/r2/d/.htindex": true},
	}
	h2 := NewFileHandler("/r2", fs2, opts)
	if _, st := h2.ResolveFile("/d/"); st == nil {
		t.Error("nondisclosure index should be NotFound")
	}
}

// TestLastBranches covers the final reachable error branches.
func TestLastBranches(t *testing.T) {
	// Empty request URI: "GET  HTTP/1.1" (two spaces) yields an empty target.
	if _, st := ParseRequest([]byte("GET  HTTP/1.1\r\n\r\n"), nil); st == nil {
		t.Error("empty URI should fail")
	}

	// A path that unescapes to a parent-escaping path -> NormalizePath fails ->
	// BadRequest in parseURI.
	if _, st := ParseRequest([]byte("GET /%2e%2e/secret HTTP/1.1\r\nHost: h\r\n\r\n"), nil); st == nil {
		t.Error("escaped .. should fail normalize")
	}

	// Chunked with a trailer header (consumed leniently, body intact).
	withTrailer := "POST /u HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nWiki\r\n0\r\nX-Trailer: v\r\n\r\n"
	if r, st := ParseRequest([]byte(withTrailer), nil); st != nil || string(r.Body) != "Wiki" {
		t.Errorf("chunk trailer = %q, %v", r.Body, st)
	}

	// Chunk size line missing entirely (EOF right after the header block).
	if _, st := ParseRequest([]byte("POST /u HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n"), nil); st == nil {
		t.Error("missing chunk size should fail")
	}

	// Overflowing hex chunk size (> int64) -> ParseInt error.
	overflow := "POST /u HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\nfffffffffffffffff\r\n"
	if _, st := ParseRequest([]byte(overflow), nil); st == nil {
		t.Error("overflow chunk size should fail")
	}

	// A main header line with no colon passes readHeader's null/size filter but
	// fails ParseHeader -> BadRequest (readHeader's ParseHeader error branch).
	if _, st := ParseRequest([]byte("GET / HTTP/1.1\r\nBadHeaderNoColon\r\n\r\n"), nil); st == nil {
		t.Error("colonless main header should fail")
	}

	// globMatch trailing-star run that must be skipped at the end.
	if !globMatch("ab**", "ab") {
		t.Error("glob trailing stars")
	}

	// validQNumber: digits then a non-dot char, and fractional with a non-digit.
	if got := ParseQValues("a;q=1x"); len(got) != 0 {
		t.Errorf("q=1x = %v", got)
	}
	if got := ParseQValues("a;q=0.x"); len(got) != 0 {
		t.Errorf("q=0.x = %v", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
