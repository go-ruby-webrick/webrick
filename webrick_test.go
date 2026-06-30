// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import (
	"strings"
	"testing"
)

// TestHTTPVersion covers parse, compare, ordering and formatting, including the
// error paths (the deterministic, ruby-free suite that holds the gate on the
// no-ruby lanes).
func TestHTTPVersion(t *testing.T) {
	v, err := ParseHTTPVersion("1.1")
	if err != nil || v.Major != 1 || v.Minor != 1 {
		t.Fatalf("ParseHTTPVersion(1.1) = %v, %v", v, err)
	}
	if v.String() != "1.1" {
		t.Errorf("String = %q", v.String())
	}
	for _, bad := range []string{"", "1", "1.", ".1", "a.b", "1.x", "x.1", "11"} {
		if _, err := ParseHTTPVersion(bad); err == nil {
			t.Errorf("ParseHTTPVersion(%q) expected error", bad)
		}
	}
	cmp := []struct {
		a, b HTTPVersion
		want int
	}{
		{HTTPVersion{1, 0}, HTTPVersion{1, 1}, -1},
		{HTTPVersion{1, 1}, HTTPVersion{1, 0}, 1},
		{HTTPVersion{2, 0}, HTTPVersion{1, 9}, 1},
		{HTTPVersion{0, 9}, HTTPVersion{1, 0}, -1},
		{HTTPVersion{1, 1}, HTTPVersion{1, 1}, 0},
	}
	for _, c := range cmp {
		if got := c.a.Compare(c.b); got != c.want {
			t.Errorf("Compare(%v,%v) = %d want %d", c.a, c.b, got, c.want)
		}
	}
	if !(HTTPVersion{1, 0}).Less(HTTPVersion{1, 1}) {
		t.Error("Less failed")
	}
}

// TestStatus covers the status table, the category classification, the Is*
// predicates, the named constructors, and the Status error/ToI methods.
func TestStatus(t *testing.T) {
	if ReasonPhrase(404) != "Not Found" || ReasonPhrase(999) != "" {
		t.Error("ReasonPhrase")
	}
	preds := []struct {
		fn   func(int) bool
		code int
		want bool
	}{
		{IsInfo, 100, true}, {IsInfo, 200, false},
		{IsSuccess, 204, true}, {IsSuccess, 100, false},
		{IsRedirect, 301, true}, {IsRedirect, 200, false},
		{IsError, 404, true}, {IsError, 200, false}, {IsError, 500, true},
		{IsClientError, 404, true}, {IsClientError, 500, false},
		{IsServerError, 503, true}, {IsServerError, 404, false},
	}
	for _, p := range preds {
		if got := p.fn(p.code); got != p.want {
			t.Errorf("predicate(%d) = %v want %v", p.code, got, p.want)
		}
	}

	// categoryForCode for all ranges + out-of-range.
	for code, want := range map[int]StatusCategory{
		150: CategoryInfo, 250: CategorySuccess, 350: CategoryRedirect,
		450: CategoryClientError, 550: CategoryServerError,
	} {
		if c, ok := categoryForCode(code); !ok || c != want {
			t.Errorf("categoryForCode(%d) = %v,%v", code, c, ok)
		}
	}
	if _, ok := categoryForCode(700); ok {
		t.Error("categoryForCode(700) should not be ok")
	}

	if NewStatus(999, "x") != nil {
		t.Error("NewStatus(999) should be nil")
	}
	st := StatusNotFound("nope")
	if st.Error() != "nope" || st.ToI() != 404 || st.Category != CategoryClientError {
		t.Errorf("StatusNotFound bad: %+v", st)
	}
	bare := StatusForbidden()
	if bare.Error() != "Forbidden" {
		t.Errorf("bare Forbidden Error = %q", bare.Error())
	}
	// Exercise every named constructor + StatusError.
	ctors := []*Status{
		StatusOK(), StatusMovedPermanently(), StatusFound(), StatusTemporaryRedirect(),
		StatusNotModified(), StatusBadRequest(), StatusForbidden(), StatusNotFound(),
		StatusMethodNotAllowed(), StatusLengthRequired(), StatusRequestEntityTooLarge(),
		StatusNotImplemented(), StatusInternalServerError(), StatusError(200),
	}
	for _, c := range ctors {
		if c == nil || c.Code == 0 {
			t.Errorf("ctor produced %v", c)
		}
	}
}

// TestHTTPUtilsDeterministic covers escape/unescape edge cases, MimeType
// branches, NormalizePath failures, EscapePath no-slash, dequote/quote,
// hexVal/hasParentRef and the q-value/range parsers.
func TestHTTPUtilsDeterministic(t *testing.T) {
	if Escape("") != "" {
		t.Error("Escape empty")
	}
	// unescapeAll trailing '%' and short escape pass through.
	if Unescape("a%2") != "a%2" || Unescape("end%") != "end%" {
		t.Errorf("Unescape short %q %q", Unescape("a%2"), Unescape("end%"))
	}
	// hexVal lowercase/uppercase/digit.
	if Unescape("%aF%0b") != "\xaf\x0b" {
		t.Errorf("Unescape hex = %q", Unescape("%aF%0b"))
	}
	if EscapePath("noslash") != "" {
		t.Errorf("EscapePath no slash = %q", EscapePath("noslash"))
	}
	// MimeType: suffix1 hit, suffix2 hit, neither.
	if MimeType("a.txt", DefaultMimeTypes) != "text/plain" {
		t.Error("mime txt")
	}
	if MimeType("a.tar.png", DefaultMimeTypes) != "image/png" {
		t.Error("mime png direct")
	}
	if MimeType("README", DefaultMimeTypes) != "application/octet-stream" {
		t.Error("mime none")
	}
	if MimeType("weird.", DefaultMimeTypes) != "application/octet-stream" {
		t.Error("mime trailing dot")
	}
	// NormalizePath failures.
	for _, bad := range []string{"", "noslash", "/../x", "/a/../../b"} {
		if _, ok := NormalizePath(bad); ok {
			t.Errorf("NormalizePath(%q) should fail", bad)
		}
	}
	if p, ok := NormalizePath("/a/b/../c"); !ok || p != "/a/c" {
		t.Errorf("NormalizePath /a/b/../c = %q,%v", p, ok)
	}
	// Dequote without quotes and Quote of plain.
	if Dequote("plain") != "plain" || Quote("ab") != "\"ab\"" {
		t.Errorf("dequote/quote: %q %q", Dequote("plain"), Quote("ab"))
	}
	if Dequote("\\x") != "x" {
		t.Errorf("dequote escape: %q", Dequote("\\x"))
	}
	// Quote's escape branch (MRI's \1=0x01 quirk): a '"' and a '\' each become
	// "\x01" preceded by a backslash.
	if Quote("a\"b\\c") != "\"a\\\x01b\\\x01c\"" {
		t.Errorf("quote escape = %q", Quote("a\"b\\c"))
	}
	// q-values: blank, malformed, no-q default.
	if len(ParseQValues("")) != 0 {
		t.Error("qv empty")
	}
	if got := ParseQValues("a, b;q=0.5"); strings.Join(got, ",") != "a,b" {
		t.Errorf("qv = %v", got)
	}
	// malformed parts are skipped.
	if got := ParseQValues("a b, c"); strings.Join(got, ",") != "c" {
		t.Errorf("qv malformed = %v", got)
	}
	if got := ParseQValues("a;q=x"); len(got) != 0 {
		t.Errorf("qv bad-q = %v", got)
	}
	if got := ParseQValues("a;notq=1"); len(got) != 0 {
		t.Errorf("qv non-q param = %v", got)
	}
	if got := ParseQValues("a;q=0."); len(got) != 0 {
		t.Errorf("qv trailing dot = %v", got)
	}
}

// TestResponseBytesDeterministic builds responses and checks their bytes
// without the MRI oracle, so the Windows/qemu (no-ruby) lanes cover the
// response builder fully.
func TestResponseBytesDeterministic(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ServerSoftware = "WEBrick/test"
	cfg.ServerName = "h"

	// plain 200 with body.
	res := NewResponse(cfg)
	res.RequestMethod = "GET"
	res.SetStatus(200)
	res.Set("content-type", "text/plain")
	res.Body = []byte("hello")
	out, err := res.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	want := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nServer: WEBrick/test\r\n" +
		"Content-Length: 5\r\nConnection: Keep-Alive\r\n\r\nhello"
	if string(out) != want {
		t.Errorf("200 bytes:\n got %q\n want %q", out, want)
	}

	// chunked body.
	res = NewResponse(cfg)
	res.RequestMethod = "GET"
	res.SetStatus(200)
	res.SetChunked(true)
	res.Body = []byte("Wiki")
	out, _ = res.Bytes()
	if !strings.Contains(string(out), "Transfer-Encoding: chunked\r\n") ||
		!strings.HasSuffix(string(out), "4\r\nWiki\r\n0\r\n\r\n") {
		t.Errorf("chunked bytes = %q", out)
	}

	// chunked with empty body still terminates.
	res = NewResponse(cfg)
	res.RequestMethod = "GET"
	res.SetStatus(200)
	res.SetChunked(true)
	res.Body = nil
	out, _ = res.Bytes()
	if !strings.HasSuffix(string(out), "0\r\n\r\n") {
		t.Errorf("empty chunked = %q", out)
	}

	// HEAD suppresses the body.
	res = NewResponse(cfg)
	res.RequestMethod = "HEAD"
	res.SetStatus(200)
	res.Set("content-type", "text/plain")
	res.Body = []byte("hello")
	out, _ = res.Bytes()
	if strings.Contains(string(out), "hello") {
		t.Error("HEAD should suppress body")
	}

	// 204 no-content clears the body.
	res = NewResponse(cfg)
	res.RequestMethod = "GET"
	res.SetStatus(204)
	res.Body = []byte("ignored")
	out, _ = res.Bytes()
	if strings.Contains(string(out), "ignored") || strings.Contains(string(out), "Content-Length") {
		t.Errorf("204 = %q", out)
	}

	// redirect: set_redirect body + Location.
	res = NewResponse(cfg)
	res.RequestMethod = "GET"
	st := res.SetRedirect(StatusFound(), "http://example.com/new")
	if st.Code != 302 {
		t.Errorf("redirect status = %d", st.Code)
	}
	res.SetStatus(st.Code)
	out, _ = res.Bytes()
	body := string(out)
	if !strings.Contains(body, "Location: http://example.com/new\r\n") ||
		!strings.Contains(body, `<A HREF="http://example.com/new">`) {
		t.Errorf("redirect = %q", out)
	}

	// error page via SetError (no ruby needed).
	res = NewResponse(cfg)
	res.RequestMethod = "GET"
	res.SetError(StatusNotFound("'/x' not found."))
	out, _ = res.Bytes()
	if !strings.HasPrefix(string(out), "HTTP/1.1 404 Not Found\r\n") ||
		!strings.Contains(string(out), "<H1>Not Found</H1>") {
		t.Errorf("error page = %q", out)
	}

	// cookie serialisation on the wire.
	res = NewResponse(cfg)
	res.RequestMethod = "GET"
	res.SetStatus(200)
	res.Set("content-type", "text/plain")
	res.Body = []byte("x")
	c := NewCookie("sid", "v")
	c.Path = "/"
	res.Cookies = append(res.Cookies, c)
	out, _ = res.Bytes()
	if !strings.Contains(string(out), "Set-Cookie: sid=v; Path=/\r\n") {
		t.Errorf("cookie wire = %q", out)
	}
}

// TestEscapeFormAndPathDeterministic covers the form/path escapers without ruby.
func TestEscapeFormAndPathDeterministic(t *testing.T) {
	if EscapeForm("a b&c=d") != "a+b%26c%3Dd" {
		t.Errorf("EscapeForm = %q", EscapeForm("a b&c=d"))
	}
	if EscapePath("/a b/c+d") != "/a%20b/c+d" {
		t.Errorf("EscapePath = %q", EscapePath("/a b/c+d"))
	}
	if Escape8bit("a\xc3\xa9") != "a%C3%A9" {
		t.Errorf("Escape8bit = %q", Escape8bit("a\xc3\xa9"))
	}
	if codeString(404) != "404" {
		t.Errorf("codeString = %q", codeString(404))
	}
	// computeKeepAlive close branch via a Connection: close request.
	r, _ := ParseRequest([]byte("GET / HTTP/1.1\r\nConnection: close\r\n\r\n"), nil)
	if r.KeepAlive() {
		t.Error("Connection: close should disable keep-alive")
	}
	// computeKeepAlive explicit keep-alive branch on an HTTP/1.0 request (would
	// otherwise default to false).
	r, _ = ParseRequest([]byte("GET / HTTP/1.0\r\nConnection: keep-alive\r\n\r\n"), nil)
	if !r.KeepAlive() {
		t.Error("Connection: keep-alive should enable keep-alive on 1.0")
	}
	// dotSegment + collapseSlashes via NormalizePath without oracle.
	if p, _ := NormalizePath("/a//b/./c"); p != "/a/b/c" {
		t.Errorf("normalize = %q", p)
	}
	// dotSegment at end-of-string ("/x/." -> "/x/").
	if p, _ := NormalizePath("/x/."); p != "/x/" {
		t.Errorf("normalize trailing dot = %q", p)
	}
	if Dequote(`"quoted"`) != "quoted" {
		t.Errorf("dequote quoted = %q", Dequote(`"quoted"`))
	}
}

// TestParseRangeHeader covers the three range forms and the failure cases.
func TestParseRangeHeader(t *testing.T) {
	rs, ok := ParseRangeHeader("bytes=0-99")
	if !ok || len(rs) != 1 || rs[0] != (ByteRange{0, 99}) {
		t.Errorf("range 0-99 = %v,%v", rs, ok)
	}
	rs, _ = ParseRangeHeader("bytes=100-")
	if rs[0] != (ByteRange{100, -1}) {
		t.Errorf("range 100- = %v", rs)
	}
	rs, _ = ParseRangeHeader("bytes=-500")
	if rs[0] != (ByteRange{-500, -1}) {
		t.Errorf("range -500 = %v", rs)
	}
	rs, _ = ParseRangeHeader("bytes=0-9,20-29")
	if len(rs) != 2 {
		t.Errorf("multi-range = %v", rs)
	}
	if _, ok := ParseRangeHeader("items=0-9"); ok {
		t.Error("non-bytes range should not match")
	}
	if _, ok := ParseRangeHeader("bytes=abc"); ok {
		t.Error("bad spec should fail")
	}
}

// TestSplitHeaderValue covers quoted commas, escapes and trailing content.
func TestSplitHeaderValue(t *testing.T) {
	got := SplitHeaderValue(`"a,b", c`)
	if len(got) != 2 || got[0] != `"a,b"` || got[1] != "c" {
		t.Errorf("split = %#v", got)
	}
	got = SplitHeaderValue(`"x\"y"`)
	if len(got) != 1 || got[0] != `"x\"y"` {
		t.Errorf("split escape = %#v", got)
	}
	if len(SplitHeaderValue("")) != 0 {
		t.Error("split empty")
	}
}

// TestCookie covers parse (with $Version/$Path/$Domain/$Port), to_s with every
// attribute, and the value-less key path.
func TestCookie(t *testing.T) {
	cookies := ParseCookies("$Version=1; a=1; $Path=/p; $Domain=d; $Port=80; b=2; flag")
	if len(cookies) != 3 {
		t.Fatalf("parsed %d cookies: %#v", len(cookies), cookies)
	}
	if cookies[0].Name != "a" || cookies[0].Value != "1" || cookies[0].Path != "/p" || cookies[0].Version != 1 {
		t.Errorf("cookie a = %+v", cookies[0])
	}
	if cookies[2].Name != "flag" || cookies[2].Value != "" {
		t.Errorf("flag cookie = %+v", cookies[2])
	}
	if ParseCookies("") != nil {
		// empty string yields a single empty-name cookie in MRI? No: split("")=[] -> no cookies.
	}

	c := NewCookie("sid", "v")
	c.Version = 2
	c.Domain = "ex.com"
	c.Expires = "Wed, 01 Jan 2030 00:00:00 GMT"
	ma := 3600
	c.MaxAge = &ma
	c.Comment = "hi"
	c.Path = "/"
	c.Secure = true
	want := "sid=v; Version=2; Domain=ex.com; Expires=Wed, 01 Jan 2030 00:00:00 GMT; Max-Age=3600; Comment=hi; Path=/; Secure"
	if c.String() != want {
		t.Errorf("cookie to_s:\n got %q\n want %q", c.String(), want)
	}
}

// TestParseQuery covers single, repeated (chained) and value-less keys.
func TestParseQuery(t *testing.T) {
	q := ParseQuery("a=1&b=2&a=3&c&=skip&d=x+y")
	if v, _ := q.Get("a"); v != "1" {
		t.Errorf("a = %q", v)
	}
	if list := q.Item("a").List(); strings.Join(list, ",") != "1,3" {
		t.Errorf("a list = %v", list)
	}
	if v, _ := q.Get("c"); v != "" {
		t.Errorf("c = %q", v)
	}
	if v, _ := q.Get("d"); v != "x y" {
		t.Errorf("d = %q", v)
	}
	if _, ok := q.Get("missing"); ok {
		t.Error("missing should be absent")
	}
	if q.Len() != 5 {
		t.Errorf("len = %d", q.Len())
	}
	if ParseQuery("").Len() != 0 {
		t.Error("empty query")
	}
}

// TestParseRequestErrors covers the malformed-request branches.
func TestParseRequestErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"bad-request-line", "GARBAGE\r\n\r\n"},
		{"no-header-terminator", "GET / HTTP/1.1\r\nHost: h\r\n"},
		{"null-in-header", "GET / HTTP/1.1\r\nX: a\x00b\r\n\r\n"},
		{"multiple-content-length", "POST / HTTP/1.1\r\nContent-Length: 1\r\nContent-Length: 2\r\n\r\nx"},
		{"bad-content-length", "POST / HTTP/1.1\r\nContent-Length: abc\r\n\r\n"},
		{"te-and-cl", "POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\nContent-Length: 1\r\n\r\nx"},
		{"unknown-te", "POST / HTTP/1.1\r\nTransfer-Encoding: gzip\r\n\r\nx"},
		{"post-no-length", "POST / HTTP/1.1\r\nHost: h\r\n\r\n"},
		{"short-body", "POST / HTTP/1.1\r\nContent-Length: 100\r\n\r\nshort"},
		{"bad-chunk-size", "POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\nXYZ\r\n"},
		{"bad-chunk-data", "POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nab\r\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, st := ParseRequest([]byte(c.raw), nil); st == nil {
				t.Errorf("expected error for %q", c.raw)
			}
		})
	}
}

// TestParseRequestVariants covers HTTP/0.9, asterisk-form, CONNECT, absolute URI,
// chunked with trailers and extensions, and the accessors.
func TestParseRequestVariants(t *testing.T) {
	// HTTP/0.9 simple request (no version, no headers).
	r, st := ParseRequest([]byte("GET /index\r\n"), nil)
	if st != nil {
		t.Fatalf("0.9 parse: %v", st)
	}
	if r.HTTPVersion.Major != 0 || r.Path != "/index" {
		t.Errorf("0.9 = %+v", r)
	}

	// asterisk-form OPTIONS.
	r, st = ParseRequest([]byte("OPTIONS * HTTP/1.1\r\nHost: h\r\n\r\n"), nil)
	if st != nil || r.UnparsedURI != "*" {
		t.Errorf("asterisk = %+v, %v", r, st)
	}

	// CONNECT.
	r, st = ParseRequest([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: h\r\n\r\n"), nil)
	if st != nil || r.RequestMethod != "CONNECT" {
		t.Errorf("connect = %+v, %v", r, st)
	}

	// absolute URI.
	r, st = ParseRequest([]byte("GET http://example.com:9000/p?q=1 HTTP/1.1\r\nHost: ignored\r\n\r\n"), nil)
	if st != nil {
		t.Fatalf("absolute: %v", st)
	}
	if r.Host() != "example.com" || r.Port() != 9000 || r.Path != "/p" || r.QueryString != "q=1" {
		t.Errorf("absolute = host=%q port=%d path=%q qs=%q", r.Host(), r.Port(), r.Path, r.QueryString)
	}

	// chunked with extension + trailer.
	raw := "POST /u HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\n" +
		"4;ext=1\r\nWiki\r\n5\r\npedia\r\n0\r\nTrailer: v\r\n\r\n"
	r, st = ParseRequest([]byte(raw), nil)
	if st != nil || string(r.Body) != "Wikipedia" {
		t.Errorf("chunked-ext body = %q, %v", r.Body, st)
	}

	// content-length query for POST form body.
	r, _ = ParseRequest([]byte("POST /f HTTP/1.1\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: 7\r\n\r\na=1&b=2"), nil)
	if v, _ := r.Query().Get("a"); v != "1" {
		t.Errorf("post query a = %q", v)
	}
	// memoised second call.
	if r.Query() == nil {
		t.Error("memoised query nil")
	}
	cl, ok := r.ContentLength()
	if !ok || cl != 7 {
		t.Errorf("content-length = %d,%v", cl, ok)
	}

	// default query branch (non-form POST yields empty).
	r, _ = ParseRequest([]byte("POST /x HTTP/1.1\r\nContent-Type: text/plain\r\nContent-Length: 3\r\n\r\nabc"), nil)
	if r.Query().Len() != 0 {
		t.Error("non-form query should be empty")
	}

	// EachHeader + Header accessors.
	r, _ = ParseRequest([]byte("GET /a HTTP/1.1\r\nHost: h\r\nX-Test: yes\r\n\r\n"), nil)
	seen := map[string]string{}
	r.EachHeader(func(k, v string) { seen[k] = v })
	if seen["x-test"] != "yes" {
		t.Errorf("each header = %v", seen)
	}
	if v, ok := r.Header("x-test"); !ok || v != "yes" {
		t.Errorf("Header() = %q,%v", v, ok)
	}
	if _, ok := r.Header("absent"); ok {
		t.Error("absent header")
	}
	if _, ok := r.ContentLength(); ok {
		t.Error("no content-length expected")
	}
}

// TestServletDispatch covers AbstractServlet (GET/HEAD/OPTIONS/unsupported) and
// ProcServlet (GET/POST/PUT/OPTIONS/unsupported).
func TestServletDispatch(t *testing.T) {
	s := NewAbstractServlet()
	s.Handle("GET", func(req *Request, res *Response) *Status {
		res.Body = []byte("got")
		return nil
	})
	s.Handle("POST", func(req *Request, res *Response) *Status { return nil })

	mk := func(method string) (*Request, *Response) {
		req := &Request{RequestMethod: method}
		return req, NewResponse(nil)
	}

	req, res := mk("GET")
	if st := s.Service(req, res); st != nil || string(res.Body) != "got" {
		t.Errorf("GET dispatch: %v %q", st, res.Body)
	}
	req, res = mk("HEAD")
	if st := s.Service(req, res); st != nil || string(res.Body) != "got" {
		t.Errorf("HEAD->GET dispatch: %v", st)
	}
	req, res = mk("OPTIONS")
	if st := s.Service(req, res); st != nil {
		t.Errorf("OPTIONS: %v", st)
	}
	if allow, _ := res.Get("allow"); !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
		t.Errorf("allow = %q", allow)
	}
	req, res = mk("DELETE")
	if st := s.Service(req, res); st == nil || st.Code != 405 {
		t.Errorf("DELETE should be 405: %v", st)
	}

	// AbstractServlet default GET (no handler) -> NotFound.
	def := NewAbstractServlet()
	req, res = mk("GET")
	if st := def.Service(req, res); st == nil || st.Code != 404 {
		t.Errorf("default GET = %v", st)
	}
	// HEAD with no GET handler falls through to MethodNotAllowed.
	bare := &AbstractServlet{handlers: map[string]HandlerFunc{}}
	req, res = mk("HEAD")
	if st := bare.Service(req, res); st == nil || st.Code != 405 {
		t.Errorf("bare HEAD = %v", st)
	}

	// ProcServlet.
	p := NewProcServlet(func(req *Request, res *Response) *Status {
		res.Body = []byte("proc:" + req.RequestMethod)
		return nil
	})
	for _, m := range []string{"GET", "POST", "PUT", "HEAD"} {
		req, res = mk(m)
		if st := p.Service(req, res); st != nil || string(res.Body) != "proc:"+m {
			t.Errorf("proc %s = %v %q", m, st, res.Body)
		}
	}
	req, res = mk("OPTIONS")
	if st := p.Service(req, res); st != nil {
		t.Errorf("proc OPTIONS = %v", st)
	}
	req, res = mk("DELETE")
	if st := p.Service(req, res); st == nil || st.Code != 405 {
		t.Errorf("proc DELETE = %v", st)
	}
}

// TestMountDispatch covers the longest-prefix routing, unmount, and the
// asterisk / not-found service paths.
func TestMountDispatch(t *testing.T) {
	srv := NewHTTPServer(nil)
	root := NewProcServlet(func(req *Request, res *Response) *Status { res.Body = []byte("root"); return nil })
	foo := NewProcServlet(func(req *Request, res *Response) *Status { res.Body = []byte("foo"); return nil })
	foobar := NewProcServlet(func(req *Request, res *Response) *Status { res.Body = []byte("foobar"); return nil })
	srv.Mount("/", root)
	srv.Mount("/foo", foo)
	srv.Mount("/foo/bar", foobar)

	cases := []struct {
		path       string
		wantBody   string
		scriptName string
		pathInfo   string
	}{
		{"/foo/bar/baz", "foobar", "/foo/bar", "/baz"},
		{"/foo/x", "foo", "/foo", "/x"},
		{"/foobar", "root", "", "/foobar"},
		{"/", "root", "", "/"},
		{"/other", "root", "", "/other"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			req := &Request{RequestMethod: "GET", Path: c.path, UnparsedURI: c.path}
			res := NewResponse(nil)
			st := srv.Service(req, res)
			if st != nil {
				t.Fatalf("service: %v", st)
			}
			if string(res.Body) != c.wantBody {
				t.Errorf("body = %q want %q", res.Body, c.wantBody)
			}
			if req.ScriptName != c.scriptName || req.PathInfo != c.pathInfo {
				t.Errorf("script=%q info=%q want %q/%q", req.ScriptName, req.PathInfo, c.scriptName, c.pathInfo)
			}
		})
	}

	// MountProc + Unmount: removing /foo/bar reroutes /foo/bar/baz to /foo.
	srv.Unmount("/foo/bar")
	req := &Request{RequestMethod: "GET", Path: "/foo/bar/baz", UnparsedURI: "/foo/bar/baz"}
	res := NewResponse(nil)
	_ = srv.Service(req, res)
	if string(res.Body) != "foo" {
		t.Errorf("after unmount body = %q", res.Body)
	}

	// SearchServlet ok flag.
	if _, _, _, ok := srv.SearchServlet("/foo"); !ok {
		t.Error("SearchServlet /foo should match")
	}

	// Empty mount table -> NotFound.
	empty := NewHTTPServer(nil)
	req = &Request{RequestMethod: "GET", Path: "/x", UnparsedURI: "/x"}
	if st := empty.Service(req, NewResponse(nil)); st == nil || st.Code != 404 {
		t.Errorf("empty service = %v", st)
	}

	// asterisk OPTIONS -> OK with Allow.
	req = &Request{RequestMethod: "OPTIONS", UnparsedURI: "*"}
	res = NewResponse(nil)
	if st := srv.Service(req, res); st == nil || st.Code != 200 {
		t.Errorf("asterisk OPTIONS = %v", st)
	}
	if allow, _ := res.Get("allow"); allow != "GET,HEAD,POST,OPTIONS" {
		t.Errorf("asterisk allow = %q", allow)
	}
	// asterisk non-OPTIONS -> NotFound.
	req = &Request{RequestMethod: "GET", UnparsedURI: "*"}
	if st := srv.Service(req, NewResponse(nil)); st == nil || st.Code != 404 {
		t.Errorf("asterisk GET = %v", st)
	}
}

// TestMountTableUnits covers MountTable.Get/Delete edge cases and normalize.
func TestMountTableUnits(t *testing.T) {
	mt := NewMountTable()
	s := NewProcServlet(func(req *Request, res *Response) *Status { return nil })
	mt.Set("/a/", s) // trailing slash normalised
	if _, ok := mt.Get("/a"); !ok {
		t.Error("Get /a after Set /a/")
	}
	if _, _, ok := mt.Scan("/none"); ok {
		t.Error("Scan empty table should miss")
	}
	if v, ok := mt.Delete("/a"); !ok || v == nil {
		t.Error("Delete /a")
	}
	if _, ok := mt.Delete("/a"); ok {
		t.Error("Delete again should miss")
	}
}

// TestResponseUnits covers the response accessors and edge branches not hit by
// the oracle: getSingle absent, Delete, chunked toggle via Set, InvalidHeader,
// SetRequest, and the 0.9 send_header (no header block).
func TestResponseUnits(t *testing.T) {
	res := NewResponse(nil)
	if _, ok := res.Get("absent"); ok {
		t.Error("absent header")
	}
	res.Set("X-A", "1")
	res.Delete("x-a")
	if _, ok := res.Get("x-a"); ok {
		t.Error("delete failed")
	}
	res.Set("transfer-encoding", "chunked")
	if !res.Chunked() {
		t.Error("Set transfer-encoding chunked should toggle")
	}
	res.SetContentType("text/x")
	res.SetContentLength(3)
	if v, _ := res.Get("content-type"); v != "text/x" {
		t.Error("content-type")
	}

	// SetRequest wiring.
	req := &Request{RequestMethod: "GET", HTTPVersion: HTTPVersion{1, 1}, host: "h", port: 81}
	req.keepAlive = true
	res2 := NewResponse(nil)
	res2.SetRequest(req)
	if res2.RequestMethod != "GET" || !res2.KeepAlive() {
		t.Error("SetRequest")
	}

	// InvalidHeader: a CR/LF in a header value.
	res3 := NewResponse(nil)
	res3.SetStatus(200)
	res3.Body = []byte("x")
	res3.header.setSingle("x-bad", "a\r\nb")
	if _, err := res3.Bytes(); err == nil {
		t.Error("expected InvalidHeader error")
	}
	var ihe = &InvalidHeaderError{Field: "x", Value: "y"}
	if ihe.Error() == "" {
		t.Error("InvalidHeaderError.Error empty")
	}

	// Cookie with CR/LF triggers invalid header on the cookie path.
	res4 := NewResponse(nil)
	res4.SetStatus(200)
	res4.Body = []byte("x")
	bad := NewCookie("a", "b\r\nc")
	res4.Cookies = append(res4.Cookies, bad)
	if _, err := res4.Bytes(); err == nil {
		t.Error("expected InvalidHeader for cookie")
	}

	// HTTP/0.9 response: no header block, just the body.
	res5 := NewResponse(nil)
	res5.RequestHTTPVersion = HTTPVersion{0, 9}
	res5.SetStatus(200)
	res5.Body = []byte("body")
	out, err := res5.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "body" {
		t.Errorf("0.9 response = %q", out)
	}

	// multipart/byteranges deletes content-length.
	res6 := NewResponse(nil)
	res6.SetStatus(206)
	res6.Set("content-type", "multipart/byteranges; boundary=x")
	res6.Body = []byte("data")
	if _, err := res6.Bytes(); err != nil {
		t.Fatal(err)
	}
	if res6.header.has("content-length") {
		t.Error("multipart should delete content-length")
	}

	// connection close path: status with body but keep_alive false and no length determinable.
	res7 := NewResponse(nil)
	res7.SetStatus(200)
	res7.SetChunked(false)
	res7.keepAlive = true
	res7.Body = nil
	if _, err := res7.Bytes(); err != nil {
		t.Fatal(err)
	}
}

// TestStatusLineRstrip checks the rstrip of an empty reason phrase.
func TestStatusLine(t *testing.T) {
	res := NewResponse(nil)
	res.Status = 200
	res.ReasonPhrase = ""
	if res.StatusLine() != "HTTP/1.1 200\r\n" {
		t.Errorf("status line = %q", res.StatusLine())
	}
}

// TestCapitalizeHeaderName covers the WWW / TE / word-boundary capitalisation.
func TestCapitalizeHeaderName(t *testing.T) {
	cases := map[string]string{
		"content-type":     "Content-Type",
		"www-authenticate": "WWW-Authenticate",
		"te":               "TE",
		"x-foo-bar":        "X-Foo-Bar",
		"etag":             "Etag",
	}
	for in, want := range cases {
		if got := capitalizeHeaderName(in); got != want {
			t.Errorf("capitalize(%q) = %q want %q", in, got, want)
		}
	}
}

// TestURIUnits covers the URI helpers' edge cases (IPv6 authority, scheme
// detection, host header port defaulting, relative non-origin failure).
func TestURIUnits(t *testing.T) {
	// IPv6 absolute URI.
	r, st := ParseRequest([]byte("GET https://[::1]:8443/p HTTP/1.1\r\nHost: h\r\n\r\n"), nil)
	if st != nil || r.Host() != "[::1]" || r.Port() != 8443 {
		t.Errorf("ipv6 = host=%q port=%d st=%v", r.Host(), r.Port(), st)
	}
	// https default port when absent.
	r, _ = ParseRequest([]byte("GET https://h/p HTTP/1.1\r\nHost: h\r\n\r\n"), nil)
	if r.Port() != 443 {
		t.Errorf("https default port = %d", r.Port())
	}
	// Plain Host header with a numeric port (the host:port parse branch).
	r, _ = ParseRequest([]byte("GET /p HTTP/1.1\r\nHost: example.com:8080\r\n\r\n"), nil)
	if r.Host() != "example.com" || r.Port() != 8080 {
		t.Errorf("host hdr port = %q:%d", r.Host(), r.Port())
	}
	// Plain Host header with a non-numeric port falls back to 80.
	r, _ = ParseRequest([]byte("GET /p HTTP/1.1\r\nHost: example.com:bad\r\n\r\n"), nil)
	if r.Host() != "example.com:bad" || r.Port() != 80 {
		t.Errorf("host hdr bad port = %q:%d", r.Host(), r.Port())
	}
	// Host header with IPv6 + port.
	r, _ = ParseRequest([]byte("GET /p HTTP/1.1\r\nHost: [::1]:9000\r\n\r\n"), nil)
	if r.Host() != "[::1]" || r.Port() != 9000 {
		t.Errorf("host hdr ipv6 = %q:%d", r.Host(), r.Port())
	}
	// Host header IPv6 no port -> 80.
	r, _ = ParseRequest([]byte("GET /p HTTP/1.1\r\nHost: [::1]\r\n\r\n"), nil)
	if r.Port() != 80 {
		t.Errorf("host ipv6 no port = %d", r.Port())
	}
	// no host header, no addr -> config ServerName/Port.
	cfg := DefaultConfig()
	cfg.ServerName = "cfg"
	cfg.Port = 8080
	r, _ = ParseRequest([]byte("GET /p HTTP/1.1\r\n\r\n"), cfg)
	if r.Host() != "cfg" || r.Port() != 8080 {
		t.Errorf("config default = %q:%d", r.Host(), r.Port())
	}
	// Escape8bitURI on.
	cfg2 := DefaultConfig()
	cfg2.Escape8bitURI = true
	if _, st := ParseRequest([]byte("GET /caf\xc3\xa9 HTTP/1.1\r\nHost: h\r\n\r\n"), cfg2); st != nil {
		t.Errorf("escape8bit uri: %v", st)
	}
	// Bad URI (relative non-origin: missing leading slash via authority form
	// that is not absolute) -> BadRequest.
	if _, st := ParseRequest([]byte("GET host:80 HTTP/1.1\r\nHost: h\r\n\r\n"), nil); st == nil {
		t.Error("relative non-origin should fail")
	}
}

// TestFileHandler covers the path-resolution core against a fake FileSystem.
func TestFileHandler(t *testing.T) {
	fs := &fakeFS{
		dirs:  map[string]bool{"/root": true, "/root/sub": true, "/root/dir": true},
		files: map[string]bool{"/root/index.html": true, "/root/sub/page.txt": true, "/root/dir/index.html": true, "/root/.htpasswd": true},
	}
	opts := DefaultFileHandlerOptions(nil)
	h := NewFileHandler("/root", fs, opts)

	// direct file.
	r, st := h.ResolveFile("/sub/page.txt")
	if st != nil || !r.Found || r.Filename != "/root/sub/page.txt" {
		t.Errorf("file resolve = %+v, %v", r, st)
	}
	// directory index.
	r, st = h.ResolveFile("/dir/")
	if st != nil || !r.Found || r.Filename != "/root/dir/index.html" {
		t.Errorf("dir index = %+v, %v", r, st)
	}
	// root index ("/").
	r, st = h.ResolveFile("/")
	if st != nil || !r.Found || r.Filename != "/root/index.html" {
		t.Errorf("root index = %+v, %v", r, st)
	}
	// missing file -> NotFound.
	if _, st := h.ResolveFile("/sub/missing.txt"); st == nil {
		t.Error("missing should be NotFound")
	}
	// nondisclosure name -> NotFound.
	if _, st := h.ResolveFile("/.htpasswd"); st == nil {
		t.Error("nondisclosure should be NotFound")
	}
	// directory with no index -> Found=false (host lists it).
	fs.dirs["/root/empty"] = true
	r, st = h.ResolveFile("/empty/")
	if st != nil || r.Found {
		t.Errorf("empty dir = %+v, %v", r, st)
	}
	// a path that descends into a subdir then stops at the dir itself.
	r, st = h.ResolveFile("/sub")
	if st != nil {
		t.Errorf("/sub resolve err: %v", st)
	}
}

// TestWindowsAmbiguousAndGlob covers the windows-ambiguous and glob matchers.
func TestWindowsAmbiguousAndGlob(t *testing.T) {
	if !windowsAmbiguousName("foo.") || !windowsAmbiguousName("foo ") || !windowsAmbiguousName("x::$DATA") {
		t.Error("windowsAmbiguous true cases")
	}
	if windowsAmbiguousName("foo") || windowsAmbiguousName("") {
		t.Error("windowsAmbiguous false cases")
	}
	if !globMatch("*.txt", "a.txt") || !globMatch(".ht*", ".htpasswd") || !globMatch("a?c", "abc") {
		t.Error("glob true cases")
	}
	if globMatch("*.txt", "a.png") || globMatch("a?c", "ac") {
		t.Error("glob false cases")
	}
	if !globMatch("*", "anything") || !globMatch("", "") {
		t.Error("glob star/empty")
	}
}

// fakeFS is a deterministic in-memory FileSystem for FileHandler tests.
type fakeFS struct {
	dirs  map[string]bool
	files map[string]bool
}

func (f *fakeFS) IsDir(p string) bool  { return f.dirs[p] }
func (f *fakeFS) IsFile(p string) bool { return f.files[p] }
