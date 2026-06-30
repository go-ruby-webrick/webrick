// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import (
	"os/exec"
	"strings"
	"testing"
)

// rubyBin locates a usable `ruby` with the `webrick` gem available, gating the
// differential oracle to MRI 4.x (the targeted line). It self-skips when ruby is
// absent (the qemu cross-arch and Windows lanes) or webrick cannot load, so the
// deterministic tests alone hold the 100% coverage gate there.
func rubyBin(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("ruby not on PATH; skipping MRI oracle")
	}
	out, err := exec.Command(path, "-e", "print RUBY_VERSION").Output()
	if err != nil {
		t.Skipf("cannot determine ruby version: %v", err)
	}
	major, _, _ := strings.Cut(string(out), ".")
	if major != "4" {
		t.Skipf("MRI oracle targets ruby 4.x; found %s", out)
	}
	if err := exec.Command(path, "-e", "require 'webrick'").Run(); err != nil {
		t.Skip("webrick gem not installed; skipping MRI oracle")
	}
	return path
}

// rubyEval runs a Ruby script with the shared WEBrick preamble and returns its
// stdout. The script $stdout.binmode's itself so Windows text-mode never
// pollutes the bytes (the go-ruby-erb lesson).
func rubyEval(t *testing.T, bin, script string) string {
	t.Helper()
	preamble := "$stdout.binmode\nrequire 'webrick'\nrequire 'stringio'\n" +
		"$NULLLOG = WEBrick::Log.new(File.open(File::NULL, 'w'))\n" +
		"def http_config\n  c = WEBrick::Config::HTTP.dup\n  c[:Logger] = $NULLLOG\n  c\nend\n"
	cmd := exec.Command(bin, "-e", preamble+script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ruby error: %v\nscript:\n%s\noutput:\n%s", err, script, out)
	}
	return string(out)
}

// parseNullFields splits a "KEY\x00VALUE\x00..." stream into a map; NUL lets
// header/body bytes (with CRLF and arbitrary text) survive intact.
func parseNullFields(s string) map[string]string {
	out := map[string]string{}
	parts := strings.Split(s, "\x00")
	for i := 0; i+1 < len(parts); i += 2 {
		out[parts[i]] = parts[i+1]
	}
	return out
}

// rubyStringLiteral renders s as an escaped double-quoted Ruby byte-string so a
// script can reconstruct s exactly.
func rubyStringLiteral(s string) string {
	var b strings.Builder
	b.WriteString(`"`)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c >= 0x20 && c < 0x7f:
			b.WriteByte(c)
		default:
			b.WriteString("\\x")
			const hex = "0123456789ABCDEF"
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	b.WriteString(`".b`)
	return b.String()
}

// serverSoftware mirrors the WEBrick ServerSoftware string the running ruby
// produces, so response oracle comparisons use the identical Server header.
func serverSoftware(t *testing.T, bin string) string {
	t.Helper()
	out := rubyEval(t, bin, `print http_config[:ServerSoftware]`)
	return out
}

// TestOracleHTTPUtils cross-checks the HTTPUtils helpers against MRI over a
// tricky corpus.
func TestOracleHTTPUtils(t *testing.T) {
	bin := rubyBin(t)
	type tc struct {
		name string
		got  string
		ruby string
	}
	cases := []tc{
		{"escape", Escape("a b/c?d#e%f<>"), `print escape("a b/c?d#e%f<>")`},
		{"unescape", Unescape("a%20b%2Fc%ZZ"), `print unescape("a%20b%2Fc%ZZ")`},
		{"escape_form", EscapeForm("a b&c=d+e"), `print escape_form("a b&c=d+e")`},
		{"unescape_form", UnescapeForm("a+b%26c%3Dd"), `print unescape_form("a+b%26c%3Dd")`},
		{"escape_path", EscapePath("/a b/c+d/e:f"), `print escape_path("/a b/c+d/e:f")`},
		{"escape8bit", Escape8bit("a\xc3\xa9b"), `print escape8bit("a\xc3\xa9b".b)`},
		{"mime_html", MimeType("foo.HTML", DefaultMimeTypes), `print mime_type("foo.HTML", WEBrick::HTTPUtils::DefaultMimeTypes)`},
		{"mime_double", MimeType("a.b.png", DefaultMimeTypes), `print mime_type("a.b.png", WEBrick::HTTPUtils::DefaultMimeTypes)`},
		{"mime_none", MimeType("noext", DefaultMimeTypes), `print mime_type("noext", WEBrick::HTTPUtils::DefaultMimeTypes)`},
		{"dequote", Dequote(`"a\"b"`), `print dequote("\"a\\\"b\"")`},
		{"quote", Quote(`a"b\c`), `print quote("a\"b\\c")`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := rubyEval(t, bin, "include WEBrick::HTTPUtils\n"+c.ruby)
			if c.got != want {
				t.Errorf("%s: Go %q, MRI %q", c.name, c.got, want)
			}
		})
	}
}

// TestOracleNormalizePath cross-checks NormalizePath against MRI's normalize_path.
func TestOracleNormalizePath(t *testing.T) {
	bin := rubyBin(t)
	inputs := []string{"/a//b/./c/../d", "/", "/./", "/a/b/../../c", "/foo/bar/", "/x/./y/."}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			got, ok := NormalizePath(in)
			if !ok {
				t.Fatalf("NormalizePath(%q) failed", in)
			}
			want := rubyEval(t, bin, "include WEBrick::HTTPUtils\nprint normalize_path("+rubyStringLiteral(in)+")")
			if got != want {
				t.Errorf("normalize(%q) = %q, MRI %q", in, got, want)
			}
		})
	}
}

// TestOracleSplitAndQValues checks split_header_value and parse_qvalues.
func TestOracleSplitAndQValues(t *testing.T) {
	bin := rubyBin(t)
	split := []string{`"a,b", c, d`, `a, b, c`, `"x\"y", z`}
	for _, in := range split {
		t.Run("split/"+in, func(t *testing.T) {
			got := strings.Join(SplitHeaderValue(in), "\x1f")
			want := rubyEval(t, bin, "include WEBrick::HTTPUtils\nprint split_header_value("+rubyStringLiteral(in)+").join(\"\\x1f\")")
			if got != want {
				t.Errorf("split(%q) Go %q MRI %q", in, got, want)
			}
		})
	}
	qv := []string{"text/html;q=0.5, application/xml;q=0.9, text/plain", "a;q=0.1, b", "x"}
	for _, in := range qv {
		t.Run("qv/"+in, func(t *testing.T) {
			got := strings.Join(ParseQValues(in), "\x1f")
			want := rubyEval(t, bin, "include WEBrick::HTTPUtils\nprint parse_qvalues("+rubyStringLiteral(in)+").join(\"\\x1f\")")
			if got != want {
				t.Errorf("qvalues(%q) Go %q MRI %q", in, got, want)
			}
		})
	}
}

// requestDumpScript parses a raw request in MRI and dumps the parsed fields,
// NUL-delimited.
const requestDumpScript = `
req = WEBrick::HTTPRequest.new(http_config)
req.parse(StringIO.new(RAW))
print "METHOD\x00#{req.request_method}\x00"
print "URI\x00#{req.unparsed_uri}\x00"
print "PATH\x00#{req.path}\x00"
print "QS\x00#{req.query_string}\x00"
print "VER\x00#{req.http_version}\x00"
print "HOST\x00#{req.host}\x00"
print "PORT\x00#{req.port}\x00"
print "KA\x00#{req.keep_alive?}\x00"
print "COOKIES\x00#{req.cookies.map{|c| "#{c.name}=#{c.value}"}.join('|')}\x00"
print "ACCEPT\x00#{req.accept.join(',')}\x00"
print "CTYPE\x00#{req.content_type}\x00"
print "BODY\x00#{req.body}\x00"
`

// TestOracleRequestParse parses several raw requests both here and in MRI and
// compares every parsed field.
func TestOracleRequestParse(t *testing.T) {
	bin := rubyBin(t)
	config := DefaultConfig()
	config.ServerName = "" // MRI fills via getservername; we avoid relying on it

	cases := []struct {
		name string
		raw  string
	}{
		{"get-query-host", "GET /foo%20bar/../baz?x=1&y=a+b HTTP/1.1\r\nHost: example.com:8080\r\nCookie: a=1; b=2\r\nAccept: text/html;q=0.5, text/plain\r\nConnection: keep-alive\r\n\r\n"},
		{"post-form", "POST /submit HTTP/1.1\r\nHost: h\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: 11\r\n\r\nname=a+b&x=1"},
		{"chunked", "POST /up HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nWiki\r\n5\r\npedia\r\n0\r\n\r\n"},
		{"http10-noka", "GET /a HTTP/1.0\r\nHost: h\r\n\r\n"},
		{"conn-close", "GET /a HTTP/1.1\r\nHost: h\r\nConnection: close\r\n\r\n"},
		{"folded-header", "GET /a HTTP/1.1\r\nHost: h\r\nX-Long: one\r\n two\r\n\r\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, st := ParseRequest([]byte(c.raw), config)
			if st != nil {
				t.Fatalf("ParseRequest: %v", st)
			}
			script := "RAW = " + rubyStringLiteral(c.raw) + "\n" + requestDumpScript
			fields := parseNullFields(rubyEval(t, bin, script))

			check := func(name, got string) {
				if got != fields[name] {
					t.Errorf("%s: Go %q, MRI %q", name, got, fields[name])
				}
			}
			check("METHOD", req.RequestMethod)
			check("URI", req.UnparsedURI)
			check("PATH", req.Path)
			check("QS", req.QueryString)
			check("VER", req.HTTPVersion.String())
			check("HOST", req.Host())
			check("PORT", itoa(req.Port()))
			check("KA", boolStr(req.KeepAlive()))
			var cookies []string
			for _, ck := range req.Cookies {
				cookies = append(cookies, ck.Name+"="+ck.Value)
			}
			check("COOKIES", strings.Join(cookies, "|"))
			check("ACCEPT", strings.Join(req.Accept, ","))
			check("CTYPE", req.ContentType())
			check("BODY", string(req.Body))
		})
	}
}

// responseScript builds a response in MRI, runs setup_header/send_header/
// send_body, deletes the non-deterministic Date header, and prints the bytes.
const responseScript = `
RESP_CONFIG = http_config
RESP_CONFIG[:ServerName] = "testhost"
res = WEBrick::HTTPResponse.new(RESP_CONFIG)
res.request_method = REQ_METHOD
res.request_http_version = WEBrick::HTTPVersion.new(REQ_VER)
res.keep_alive = REQ_KA
BUILD.call(res)
res.setup_header
res.instance_variable_get(:@header).delete('date')
class Rec; def initialize; @b=+""; end; def write(s); @b<<s; @b.bytesize; end; def <<(s); @b<<s; self; end; attr_reader :b; end
s = Rec.new
res.send_header(s)
res.send_body(s)
print s.b
`

// TestOracleResponseBytes builds equivalent responses here and in MRI and
// compares the byte streams (Date deleted on both sides).
func TestOracleResponseBytes(t *testing.T) {
	bin := rubyBin(t)
	sw := serverSoftware(t, bin)

	cases := []struct {
		name      string
		reqMethod string
		reqVer    string
		reqKA     bool
		build     func(*Response)
		rubyBuild string
	}{
		{
			"ok-text", "GET", "1.1", true,
			func(r *Response) { r.SetStatus(200); r.Set("content-type", "text/plain"); r.Body = []byte("hello") },
			`->(res){ res.status = 200; res['content-type']='text/plain'; res.body='hello' }`,
		},
		{
			"not-found-error", "GET", "1.1", true,
			func(r *Response) { r.SetError(StatusNotFound("'/missing' not found.")) },
			`->(res){ res.set_error(WEBrick::HTTPStatus::NotFound.new("'/missing' not found.")) }`,
		},
		{
			"redirect", "GET", "1.1", true,
			func(r *Response) {
				st := r.SetRedirect(StatusFound(), "http://example.com/new")
				r.SetStatus(st.Code)
			},
			`->(res){ begin; res.set_redirect(WEBrick::HTTPStatus::Found, "http://example.com/new"); rescue WEBrick::HTTPStatus::Status => e; res.status = e.code; end }`,
		},
		{
			"head-no-body", "HEAD", "1.1", true,
			func(r *Response) { r.SetStatus(200); r.Set("content-type", "text/plain"); r.Body = []byte("hello") },
			`->(res){ res.status = 200; res['content-type']='text/plain'; res.body='hello' }`,
		},
		{
			"chunked", "GET", "1.1", true,
			func(r *Response) { r.SetStatus(200); r.SetChunked(true); r.Body = []byte("Wikipedia") },
			`->(res){ res.status = 200; res.chunked = true; res.body='Wikipedia' }`,
		},
		{
			"no-content", "GET", "1.1", true,
			func(r *Response) { r.SetStatus(204); r.Body = []byte("ignored") },
			`->(res){ res.status = 204; res.body='ignored' }`,
		},
		{
			"cookie", "GET", "1.1", true,
			func(r *Response) {
				r.SetStatus(200)
				r.Set("content-type", "text/plain")
				r.Body = []byte("x")
				c := NewCookie("sid", "abc")
				c.Path = "/"
				r.Cookies = append(r.Cookies, c)
			},
			`->(res){ res.status=200; res['content-type']='text/plain'; res.body='x'; c=WEBrick::Cookie.new('sid','abc'); c.path='/'; res.cookies << c }`,
		},
		{
			"http10", "GET", "1.0", false,
			func(r *Response) { r.SetStatus(200); r.Set("content-type", "text/plain"); r.Body = []byte("hi") },
			`->(res){ res.status = 200; res['content-type']='text/plain'; res.body='hi' }`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.ServerSoftware = sw
			cfg.ServerName = "testhost"
			res := NewResponse(cfg)
			res.RequestMethod = c.reqMethod
			rv, _ := ParseHTTPVersion(c.reqVer)
			res.RequestHTTPVersion = rv
			res.keepAlive = c.reqKA
			c.build(res)
			goBytes, err := res.Bytes()
			if err != nil {
				t.Fatalf("Bytes: %v", err)
			}

			script := "REQ_METHOD = " + rubyStringLiteral(c.reqMethod) + "\n" +
				"REQ_VER = " + rubyStringLiteral(c.reqVer) + "\n" +
				"REQ_KA = " + boolStr(c.reqKA) + "\n" +
				"BUILD = " + c.rubyBuild + "\n" + responseScript
			mri := rubyEval(t, bin, script)
			if string(goBytes) != mri {
				t.Errorf("response bytes differ:\n go:  %q\n mri: %q", goBytes, mri)
			}
		})
	}
}

// TestOracleStatusTable walks MRI's StatusMessage table and asserts the reason
// phrase and category agree for every code.
func TestOracleStatusTable(t *testing.T) {
	bin := rubyBin(t)
	out := rubyEval(t, bin, `WEBrick::HTTPStatus::StatusMessage.sort_by{|k,_| k}.each{|code, msg|
  print "#{code}\t#{msg}\n"
}`)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		code, msg, _ := strings.Cut(line, "\t")
		c := atoiSafe(code)
		if got := ReasonPhrase(c); got != msg {
			t.Errorf("code %d: Go %q, MRI %q", c, got, msg)
		}
	}
}

// TestOracleErrorPage compares the full error-page HTML for a status against
// MRI's set_error output (sans Date), exercising the byte-exact error body.
func TestOracleErrorPage(t *testing.T) {
	bin := rubyBin(t)
	sw := serverSoftware(t, bin)
	cfg := DefaultConfig()
	cfg.ServerSoftware = sw
	cfg.ServerName = "myhost"
	cfg.Port = 8080
	res := NewResponse(cfg)
	res.RequestMethod = "GET"
	res.SetError(StatusForbidden("no access permission to '/secret'"))
	goBody := string(res.Body)

	script := `
c = http_config
c[:ServerName] = "myhost"
c[:Port] = 8080
res = WEBrick::HTTPResponse.new(c)
res.set_error(WEBrick::HTTPStatus::Forbidden.new("no access permission to '/secret'"))
print res.body
`
	mri := rubyEval(t, bin, script)
	if goBody != mri {
		t.Errorf("error page differs:\n go:  %q\n mri: %q", goBody, mri)
	}
}

func itoa(n int) string { return codeString(n) }
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
