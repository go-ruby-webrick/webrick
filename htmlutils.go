// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import "strings"

// HTMLEscape is the Go port of WEBrick::HTMLUtils.escape: it escapes &, ", >
// and < (in that exact replacement order) so a value is safe inside HTML. A
// nil-equivalent (here the empty string is the only nil-equivalent) yields "".
func HTMLEscape(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return s
}
