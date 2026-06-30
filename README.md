<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-webrick/brand/main/social/go-ruby-webrick-webrick.png" alt="go-ruby-webrick/webrick" width="720"></p>

# webrick — go-ruby-webrick

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-webrick.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) reimplementation of the deterministic core of Ruby's
[WEBrick](https://github.com/ruby/webrick) HTTP server** — MRI 4.0.x's
`WEBrick` request parse, response build, `HTTPStatus` table + error pages, the
servlet / mount dispatch model, and the `HTTPUtils` helpers — **without any
Ruby runtime, and without doing any socket I/O itself**.

It is the WEBrick backend for
[go-embedded-ruby](https://github.com/go-embedded-ruby/ruby) and builds on
[go-ruby-net-http](https://github.com/go-ruby-net-http/net-http) for the shared
HTTP/1.1 message vocabulary, a sibling of
[go-ruby-rack](https://github.com/go-ruby-rack/rack),
[go-ruby-regexp](https://github.com/go-ruby-regexp/regexp) and
[go-ruby-erb](https://github.com/go-ruby-erb/erb).

> **The TCP accept loop, the thread-per-connection model and the FileHandler
> filesystem reads are host-side seams.** Parsing a raw request byte stream,
> building the response bytes, the status table and the default error pages, and
> the longest-prefix mount dispatch are all deterministic and need **no
> interpreter**, so they live here as pure Go. `TCPServer.accept`, the worker
> threads, and reading a file off disk for `FileHandler` are the host's job
> (rbgo supplies them): hand `ParseRequest` everything read from the socket,
> route with `HTTPServer.Service`, and write `Response.Bytes` back.

## Features

Faithful port of WEBrick's request parse + response build, validated
byte-for-byte against the `webrick` gem on every supported platform:

- **`HTTPRequest`** — `ParseRequest` turns a raw request byte stream into the
  `request_method` / `unparsed_uri` / `path` / `query_string` / `http_version`,
  the folded multi-value header model, `host` / `port`, the body
  (`Content-Length` *and* chunked, with extensions + trailers), the parsed
  `query` (GET query string or `x-www-form-urlencoded` body), the `cookies`,
  the `Accept*` q-value lists, `keep_alive?`, and `[]` header access.
- **`HTTPResponse`** — `Response.Bytes` runs `setup_header` + `send_header` +
  `send_body`: the `status_line`, the WEBrick header capitalisation
  (`WWW-Authenticate` / `TE` / word boundaries), `Content-Length` vs chunked
  framing, the Keep-Alive / Connection decision, `set_redirect`, `Set-Cookie`,
  and the default error-page HTML. (The non-deterministic `Date` header is the
  host's to add.)
- **`HTTPStatus`** — the exact `StatusMessage` code→reason table, the
  `Info`/`Success`/`Redirect`/`ClientError`/`ServerError` category hierarchy,
  and the `Status` exception family (`NotFound` / `Forbidden` / …) servlets
  raise.
- **`HTTPServer` mount model** — `Mount` / `MountProc` / `Unmount` plus the
  `Service` dispatch: **longest-prefix path match** at a path boundary →
  servlet, with `script_name` / `path_info` split (`MountTable`).
- **Servlets** — `AbstractServlet` (the `do_<METHOD>` contract, HEAD→GET,
  OPTIONS Allow list, MethodNotAllowed), `ProcServlet` (`mount_proc`), and
  `FileHandler`'s pure path-resolution core (the `path_info`→file mapping,
  directory descent, index search, nondisclosure / Windows-ambiguous checks)
  over a host-supplied `FileSystem` seam.
- **`HTTPUtils`** — `Escape`/`Unescape`, `EscapeForm`/`UnescapeForm`,
  `EscapePath`, `Escape8bit`, `MimeType` (the `DefaultMimeTypes` table),
  `ParseQuery`, `ParseHeader`, `SplitHeaderValue`, `ParseQValues`,
  `ParseRangeHeader`, `NormalizePath`, `Dequote`/`Quote`, plus `Cookie` and
  `HTMLEscape`.

CGO-free, **100% test coverage**, `gofmt` + `go vet` clean, and green across the
six 64-bit Go targets (amd64, arm64, riscv64, loong64, ppc64le, s390x) and three
operating systems (Linux, macOS, Windows).

## Install

```sh
go get github.com/go-ruby-webrick/webrick
```

## Usage

```go
package main

import (
	"fmt"

	webrick "github.com/go-ruby-webrick/webrick"
)

func main() {
	cfg := webrick.DefaultConfig()
	cfg.ServerName = "example.com"

	// Route by longest-prefix mount, exactly like WEBrick::HTTPServer.
	srv := webrick.NewHTTPServer(cfg)
	srv.MountProc("/hello", func(req *webrick.Request, res *webrick.Response) *webrick.Status {
		res.SetStatus(200)
		res.SetContentType("text/plain")
		res.Body = []byte("hi " + req.Path)
		return nil
	})

	// The host reads a request off the socket and hands the bytes here:
	req, st := webrick.ParseRequest([]byte(
		"GET /hello?x=1 HTTP/1.1\r\nHost: example.com\r\n\r\n"), cfg)
	if st != nil { /* turn the raised status into an error response */ }

	res := webrick.NewResponse(cfg)
	res.SetRequest(req)
	if st := srv.Service(req, res); st != nil {
		res.SetError(st) // default error page + status
	}

	wire, _ := res.Bytes() // ... the host writes these bytes to the socket.
	fmt.Printf("%q\n", wire)
}
```

## The socket / thread / filesystem seams

This library is the deterministic core only; the I/O is the host's:

| Stage                              | Owner          | This library                                   |
| ---------------------------------- | -------------- | ---------------------------------------------- |
| `TCPServer.accept`, worker threads | host (rbgo)    | —                                              |
| read request bytes from the socket | host (rbgo)    | —                                              |
| parse the request                  | this library   | `ParseRequest([]byte, *Config)`                |
| route to a servlet                 | this library   | `HTTPServer.Service` (longest-prefix mount)    |
| `FileHandler` file resolution      | this library   | `FileHandler.ResolveFile` (over a `FileSystem`)|
| read the resolved file off disk    | host (rbgo)    | —                                              |
| build the response bytes           | this library   | `Response.Bytes()`                             |
| write response bytes to the socket | host (rbgo)    | —                                              |

`ParseRequest` and `Response.Bytes` never touch the network, a file, or a clock,
so the core is fully deterministic and testable in isolation — exactly how the
host drives it from any byte transport. (The `Date` response header and the
`FileHandler` file body are the two host-supplied, non-deterministic pieces.)

## Tests & coverage

The suite pairs deterministic, ruby-free tests (which alone hold coverage at
100%, so the qemu cross-arch and Windows lanes pass the gate) with a
**differential MRI oracle**: the same requests are parsed here and by the
system `ruby` (`WEBrick::HTTPRequest#parse`), responses are built here and by
`WEBrick::HTTPResponse` (`setup_header`/`send_header`/`send_body`, with the
non-deterministic `Date` deleted on both sides), and the `HTTPUtils` helpers,
the status table and the error pages are compared **byte-for-byte**. The oracle
scripts `$stdout.binmode` so Windows text-mode never pollutes the bytes, and
skip themselves where `ruby` (or the `webrick` gem) is absent.

```sh
COVERPKG=$(go list ./... | paste -sd, -)
go test -race -coverpkg="$COVERPKG" -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # 100.0%
```

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-webrick/webrick authors.
