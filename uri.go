// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import (
	"strconv"
	"strings"
)

// parseURI is the Go port of WEBrick::HTTPRequest#parse_uri followed by the
// path/host/port/query extraction in #parse. It handles the two request-target
// forms WEBrick sees: origin-form ("/path?query", the common case) and
// absolute-form ("http://host:port/path?query"). The host/port for an
// origin-form target come from the Host header, else (the socket addr being a
// host seam) the config ServerName/Port. It sets Path (unescaped + normalised),
// host, port, QueryString, ScriptName and PathInfo, or returns BadRequest.
func (r *Request) parseURI() *Status {
	str := r.UnparsedURI
	if r.config.Escape8bitURI {
		str = Escape8bit(str)
	}
	// str.sub!(%r{\A/+}, '/') — collapse a leading run of slashes to one.
	str = collapseLeadingSlashes(str)

	scheme, host, port, path, query, ok := splitURI(str)
	if !ok {
		return StatusBadRequest("bad URI '" + r.UnparsedURI + "'.")
	}

	if scheme == "" {
		// relative URI: derive host/port from Host header or config.
		if hostHdr, present := r.Header("host"); present {
			h, p := parseHostHeader(hostHdr)
			host = h
			port = p
		} else {
			host = r.config.ServerName
			port = r.config.Port
		}
	}
	if port == 0 {
		port = defaultPortForScheme(scheme)
	}

	decodedPath := Unescape(path)
	norm, nok := NormalizePath(decodedPath)
	if !nok {
		return StatusBadRequest("bad URI '" + r.UnparsedURI + "'.")
	}
	r.Path = norm
	r.host = host
	r.port = port
	r.QueryString = query
	r.ScriptName = ""
	r.PathInfo = norm
	return nil
}

// collapseLeadingSlashes replaces a leading run of '/' with a single '/'.
func collapseLeadingSlashes(s string) string {
	if !strings.HasPrefix(s, "/") {
		return s
	}
	i := 0
	for i < len(s) && s[i] == '/' {
		i++
	}
	return "/" + s[i:]
}

// splitURI parses an absolute or relative request target into its components.
// For an absolute URI the scheme is non-empty and host/port are taken from the
// authority; for a relative URI scheme/host are empty and port is 0. ok=false
// on a target that is neither (e.g. a bare authority with no leading '/').
func splitURI(str string) (scheme, host string, port int, path, query string, ok bool) {
	rest := str
	// scheme: ALPHA *(ALPHA / DIGIT / "+" / "-" / ".") ":" "//"
	if i := schemeEnd(rest); i > 0 && strings.HasPrefix(rest[i:], "://") {
		scheme = strings.ToLower(rest[:i])
		rest = rest[i+3:]
		// authority up to '/', '?', or end
		auth := rest
		if j := strings.IndexAny(rest, "/?"); j >= 0 {
			auth = rest[:j]
			rest = rest[j:]
		} else {
			rest = ""
		}
		host, port, ok = splitAuthority(auth)
		if !ok {
			return "", "", 0, "", "", false
		}
		path, query = splitPathQuery(rest)
		if path == "" {
			path = "/"
		}
		return scheme, host, port, path, query, true
	}

	// relative: must be origin-form starting with '/'.
	if !strings.HasPrefix(rest, "/") {
		return "", "", 0, "", "", false
	}
	path, query = splitPathQuery(rest)
	return "", "", 0, path, query, true
}

// schemeEnd returns the length of a leading URI scheme token, or 0 if the
// string does not begin with a valid scheme.
func schemeEnd(s string) int {
	if s == "" || !isAlpha(s[0]) {
		return 0
	}
	i := 1
	for i < len(s) {
		c := s[i]
		if isAlpha(c) || (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.' {
			i++
			continue
		}
		break
	}
	if i < len(s) && s[i] == ':' {
		return i
	}
	return 0
}

func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

// splitAuthority parses "host" or "host:port" (also "[v6]:port"); ok=false on a
// non-numeric port.
func splitAuthority(auth string) (host string, port int, ok bool) {
	if strings.HasPrefix(auth, "[") {
		end := strings.IndexByte(auth, ']')
		if end < 0 {
			return "", 0, false
		}
		host = auth[:end+1]
		rest := auth[end+1:]
		if rest == "" {
			return host, 0, true
		}
		if rest[0] != ':' {
			return "", 0, false
		}
		port, ok = parsePort(rest[1:])
		return host, port, ok
	}
	if i := strings.LastIndexByte(auth, ':'); i >= 0 {
		host = auth[:i]
		port, ok = parsePort(auth[i+1:])
		return host, port, ok
	}
	return auth, 0, true
}

func parsePort(s string) (int, bool) {
	if s == "" {
		return 0, true
	}
	if !allDigits(s) {
		return 0, false
	}
	n, _ := strconv.Atoi(s)
	return n, true
}

// splitPathQuery splits an origin-form "/path?query" into its parts.
func splitPathQuery(s string) (path, query string) {
	if i := strings.IndexByte(s, '?'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func defaultPortForScheme(scheme string) int {
	switch scheme {
	case "https":
		return 443
	default:
		return 80
	}
}

// parseHostHeader splits a Host header into host and port, mirroring
// HTTPRequest#parse_host_request_line (HOST_PATTERN). A missing port yields the
// scheme default (80), as URI#port would.
func parseHostHeader(hostHdr string) (string, int) {
	hostHdr = strings.TrimSpace(hostHdr)
	if strings.HasPrefix(hostHdr, "[") {
		end := strings.IndexByte(hostHdr, ']')
		if end >= 0 {
			host := hostHdr[:end+1]
			rest := hostHdr[end+1:]
			if strings.HasPrefix(rest, ":") && allDigits(rest[1:]) && len(rest) > 1 {
				n, _ := strconv.Atoi(rest[1:])
				return host, n
			}
			return host, 80
		}
	}
	if i := strings.LastIndexByte(hostHdr, ':'); i >= 0 {
		portStr := hostHdr[i+1:]
		if allDigits(portStr) && portStr != "" {
			n, _ := strconv.Atoi(portStr)
			return hostHdr[:i], n
		}
	}
	return hostHdr, 80
}
