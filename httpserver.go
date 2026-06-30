// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import (
	"sort"
	"strings"
)

// HTTPServer is the pure-Go port of the WEBrick::HTTPServer mount/dispatch
// model: the mount registry plus the service() dispatch (longest-prefix path
// match to a servlet). The TCPServer.accept loop, the thread-per-connection
// model, the access log and the socket I/O are host-side seams (rbgo wires
// them); this type owns only the deterministic routing.
type HTTPServer struct {
	config   *Config
	mountTab *MountTable
	httpVer  HTTPVersion
}

// NewHTTPServer creates a server with the given config (defaults applied),
// mirroring HTTPServer#initialize sans the networking setup.
func NewHTTPServer(config *Config) *HTTPServer {
	if config == nil {
		config = DefaultConfig()
	}
	return &HTTPServer{
		config:   config,
		mountTab: NewMountTable(),
		httpVer:  config.HTTPVersion,
	}
}

// Mount mounts servlet on dir (HTTPServer#mount).
func (s *HTTPServer) Mount(dir string, servlet Servlet) {
	s.mountTab.Set(dir, servlet)
}

// MountProc mounts a proc on dir (HTTPServer#mount_proc).
func (s *HTTPServer) MountProc(dir string, proc HandlerFunc) {
	s.mountTab.Set(dir, NewProcServlet(proc))
}

// Unmount removes the servlet at dir (HTTPServer#unmount / #umount).
func (s *HTTPServer) Unmount(dir string) { s.mountTab.Delete(dir) }

// SearchServlet finds the servlet for path, mirroring HTTPServer#search_servlet:
// it returns the longest-prefix-matched servlet plus the script_name (the
// matched mount prefix) and path_info (the remainder). ok=false when nothing
// matches.
func (s *HTTPServer) SearchServlet(path string) (servlet Servlet, scriptName, pathInfo string, ok bool) {
	scriptName, pathInfo, matched := s.mountTab.Scan(path)
	if !matched {
		return nil, "", "", false
	}
	// Scan only returns a prefix that is a live key, so Get always succeeds.
	servlet, ok = s.mountTab.Get(scriptName)
	return servlet, scriptName, pathInfo, ok
}

// Service is the Go port of HTTPServer#service: asterisk-form OPTIONS is
// answered with the default Allow list; otherwise the request path is matched
// to a servlet by longest prefix, script_name/path_info are filled in, and the
// servlet's Service is invoked. It returns a *Status to "raise" (NotFound when
// no servlet matches, OK after an asterisk OPTIONS, or whatever the servlet
// raises); nil means the response was filled normally.
func (s *HTTPServer) Service(req *Request, res *Response) *Status {
	if req.UnparsedURI == "*" {
		if req.RequestMethod == "OPTIONS" {
			res.Set("allow", "GET,HEAD,POST,OPTIONS")
			return StatusOK()
		}
		return StatusNotFound("'" + req.UnparsedURI + "' not found.")
	}

	servlet, scriptName, pathInfo, ok := s.SearchServlet(req.Path)
	if !ok {
		return StatusNotFound("'" + req.Path + "' not found.")
	}
	req.ScriptName = scriptName
	req.PathInfo = pathInfo
	return servlet.Service(req, res)
}

// MountTable is the Go port of WEBrick::HTTPServer::MountTable: the registry of
// mount-point -> servlet, with the longest-prefix scanner. A path matches a
// mount prefix only at a path boundary (the prefix is followed by '/' or end of
// path), and the longest matching prefix wins.
type MountTable struct {
	tab map[string]Servlet
}

// NewMountTable returns an empty mount table.
func NewMountTable() *MountTable {
	return &MountTable{tab: map[string]Servlet{}}
}

// Set mounts val at dir (MountTable#[]=). dir is normalised (trailing slashes
// stripped) so "/foo/" and "/foo" are the same mount point.
func (m *MountTable) Set(dir string, val Servlet) {
	m.tab[normalizeMount(dir)] = val
}

// Get returns the servlet mounted exactly at the normalised dir (MountTable#[]).
func (m *MountTable) Get(dir string) (Servlet, bool) {
	v, ok := m.tab[normalizeMount(dir)]
	return v, ok
}

// Delete removes the mount at dir (MountTable#delete).
func (m *MountTable) Delete(dir string) (Servlet, bool) {
	d := normalizeMount(dir)
	v, ok := m.tab[d]
	delete(m.tab, d)
	return v, ok
}

// Scan finds the longest mount prefix matching path at a path boundary,
// returning the matched prefix (script_name), the remainder (path_info), and
// whether anything matched, mirroring MountTable#scan over the compiled
// \A(<prefix>|...)(?=/|\z) scanner. Keys are tried longest-first.
func (m *MountTable) Scan(path string) (scriptName, pathInfo string, ok bool) {
	keys := make([]string, 0, len(m.tab))
	for k := range m.tab {
		keys = append(keys, k)
	}
	// Sort then reverse so longer/lexically-greater prefixes win, exactly as
	// MountTable#compile does (sort!, reverse!).
	sort.Strings(keys)
	for i := len(keys) - 1; i >= 0; i-- {
		k := keys[i]
		if matchMountPrefix(path, k) {
			return k, path[len(k):], true
		}
	}
	return "", "", false
}

// matchMountPrefix reports whether key is a prefix of path ending at a path
// boundary: \A<key>(?=/|\z). The empty mount "" matches when the remainder
// starts with '/' or is empty (i.e. always, for an absolute path).
func matchMountPrefix(path, key string) bool {
	if !strings.HasPrefix(path, key) {
		return false
	}
	rest := path[len(key):]
	return rest == "" || rest[0] == '/'
}

// normalizeMount strips a trailing run of '/' from dir (MountTable#normalize:
// dir.sub(%r|/+\z|, "")). A nil dir is "".
func normalizeMount(dir string) string {
	end := len(dir)
	for end > 0 && dir[end-1] == '/' {
		end--
	}
	return dir[:end]
}
