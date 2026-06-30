// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import (
	"errors"
	"strconv"
	"strings"
)

// HTTPVersion is the pure-Go port of WEBrick::HTTPVersion: a comparable
// (major, minor) HTTP protocol version. It mirrors the parse, the to_s
// formatting, and the Comparable <=> ordering MRI exposes.
type HTTPVersion struct {
	Major int
	Minor int
}

// ParseHTTPVersion parses a "major.minor" string into an HTTPVersion, mirroring
// WEBrick::HTTPVersion#initialize: only the exact /^(\d+)\.(\d+)$/ form is
// accepted, anything else raises ArgumentError (here an error).
func ParseHTTPVersion(version string) (HTTPVersion, error) {
	maj, min, ok := splitVersion(version)
	if !ok {
		return HTTPVersion{}, errors.New("cannot convert String into WEBrick::HTTPVersion")
	}
	return HTTPVersion{Major: maj, Minor: min}, nil
}

// splitVersion applies the /^(\d+)\.(\d+)$/ match to s, returning the parsed
// major and minor and ok=false on any mismatch.
func splitVersion(s string) (maj, min int, ok bool) {
	dot := strings.IndexByte(s, '.')
	if dot <= 0 || dot == len(s)-1 {
		return 0, 0, false
	}
	a, b := s[:dot], s[dot+1:]
	if !allDigits(a) || !allDigits(b) {
		return 0, 0, false
	}
	maj, _ = strconv.Atoi(a)
	min, _ = strconv.Atoi(b)
	return maj, min, true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Compare reports -1, 0 or +1 ordering against other, mirroring
// WEBrick::HTTPVersion#<=>: compare major first, then minor.
func (v HTTPVersion) Compare(other HTTPVersion) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	return 0
}

// Less reports v < other (the HTTP-version ordering used throughout WEBrick).
func (v HTTPVersion) Less(other HTTPVersion) bool { return v.Compare(other) < 0 }

// String formats the version as "major.minor" (WEBrick::HTTPVersion#to_s).
func (v HTTPVersion) String() string {
	return strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor)
}
