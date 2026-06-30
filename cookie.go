// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import (
	"strconv"
	"strings"
)

// Cookie is the Go port of WEBrick::Cookie: a name/value pair plus the optional
// attributes (Version / Domain / Path / Secure / Comment / Max-Age / Expires).
// Its String method serialises the cookie exactly as Cookie#to_s does, in the
// same attribute order, which is the bytes a Set-Cookie response header carries.
type Cookie struct {
	Name    string
	Value   string
	Version int
	Domain  string
	Path    string
	Secure  bool
	Comment string
	MaxAge  *int   // nil = unset
	Expires string // already formatted (httpdate or raw); "" = unset
	Port    string
}

// NewCookie creates a cookie with the given name and value (Cookie.new); the
// version defaults to 0 (a Netscape cookie).
func NewCookie(name, value string) *Cookie {
	return &Cookie{Name: name, Value: value, Version: 0}
}

// String renders the cookie for an HTTP header, mirroring Cookie#to_s: the
// attributes are appended in the order Version, Domain, Expires, Max-Age,
// Comment, Path, Secure, each only when set.
func (c *Cookie) String() string {
	var b strings.Builder
	b.WriteString(c.Name)
	b.WriteByte('=')
	b.WriteString(c.Value)
	if c.Version > 0 {
		b.WriteString("; Version=")
		b.WriteString(strconv.Itoa(c.Version))
	}
	if c.Domain != "" {
		b.WriteString("; Domain=")
		b.WriteString(c.Domain)
	}
	if c.Expires != "" {
		b.WriteString("; Expires=")
		b.WriteString(c.Expires)
	}
	if c.MaxAge != nil {
		b.WriteString("; Max-Age=")
		b.WriteString(strconv.Itoa(*c.MaxAge))
	}
	if c.Comment != "" {
		b.WriteString("; Comment=")
		b.WriteString(c.Comment)
	}
	if c.Path != "" {
		b.WriteString("; Path=")
		b.WriteString(c.Path)
	}
	if c.Secure {
		b.WriteString("; Secure")
	}
	return b.String()
}

// ParseCookies parses a Cookie request-header value into cookies, mirroring
// WEBrick::Cookie.parse: split on /;\s+/, with the $Version / $Path / $Domain /
// $Port directives applying to the cookies that follow.
func ParseCookies(str string) []*Cookie {
	var ret []*Cookie
	var cookie *Cookie
	ver := 0
	for _, x := range splitSemiWS(str) {
		key, val := splitCookiePair(x)
		switch key {
		case "$Version":
			ver = atoiSafe(val)
		case "$Path":
			if cookie != nil {
				cookie.Path = val
			}
		case "$Domain":
			if cookie != nil {
				cookie.Domain = val
			}
		case "$Port":
			if cookie != nil {
				cookie.Port = val
			}
		default:
			if cookie != nil {
				ret = append(ret, cookie)
			}
			cookie = NewCookie(key, val)
			cookie.Version = ver
		}
	}
	if cookie != nil {
		ret = append(ret, cookie)
	}
	return ret
}

// splitCookiePair splits "k=v" on the first '='. A value-less key yields "";
// MRI dequotes the value (val ? dequote(val) : "").
func splitCookiePair(x string) (key, val string) {
	i := strings.IndexByte(x, '=')
	if i < 0 {
		return x, ""
	}
	return x[:i], Dequote(x[i+1:])
}

// splitSemiWS splits on /;\s+/ (the Cookie.parse separator).
func splitSemiWS(s string) []string {
	var out []string
	i := 0
	start := 0
	for i < len(s) {
		if s[i] == ';' && i+1 < len(s) && isASCIISpace(s[i+1]) {
			out = append(out, s[start:i])
			i++
			for i < len(s) && isASCIISpace(s[i]) {
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

func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

func atoiSafe(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			break
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
