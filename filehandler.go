// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import (
	"path"
	"strings"
)

// FileSystem is the host-side seam for FileHandler: the actual stat/read of the
// filesystem. FileHandler's path-resolution logic (the path_info -> file
// mapping, the directory-walk, the index-file search, the nondisclosure and
// Windows-ambiguous checks) is pure compute and lives here; the host supplies a
// FileSystem so rbgo wires the real os filesystem (or any virtual FS) in.
type FileSystem interface {
	// IsDir reports whether the given expanded path is a directory
	// (File.directory?).
	IsDir(p string) bool
	// IsFile reports whether the given expanded path is a regular file
	// (File.file?).
	IsFile(p string) bool
}

// FileHandlerOptions ports the FileHandler config keys the path resolver reads.
type FileHandlerOptions struct {
	// NondisclosureName is the glob list of names never served ([".ht*","*~"]).
	NondisclosureName []string
	// DirectoryIndex is the index-file search order (from Config).
	DirectoryIndex []string
}

// DefaultFileHandlerOptions returns WEBrick's FileHandler defaults.
func DefaultFileHandlerOptions(config *Config) FileHandlerOptions {
	if config == nil {
		config = DefaultConfig()
	}
	return FileHandlerOptions{
		NondisclosureName: []string{".ht*", "*~"},
		DirectoryIndex:    config.DirectoryIndex,
	}
}

// FileHandler is the Go port of WEBrick::HTTPServlet::FileHandler's pure
// path-resolution core. It maps a request's path_info to a filesystem path
// under root, using the supplied FileSystem for stat decisions. The actual file
// read (res.body = File.open(...)) is a host seam: ResolveFile returns the
// resolved filesystem path, and the host opens and streams it.
type FileHandler struct {
	root    string
	fs      FileSystem
	options FileHandlerOptions
}

// NewFileHandler creates a FileHandler serving files under root, resolving via
// fs (FileHandler#initialize). root should already be an absolute/expanded
// path (File.expand_path(root) is the host's responsibility, like the FS).
func NewFileHandler(root string, fs FileSystem, options FileHandlerOptions) *FileHandler {
	return &FileHandler{root: root, fs: fs, options: options}
}

// FileResolution is the outcome of ResolveFile: Filename is the resolved
// filesystem path (when Found), ScriptName/PathInfo are the updated CGI vars
// (script_name accumulates the consumed segments, path_info the remainder).
type FileResolution struct {
	Found      bool
	Filename   string
	ScriptName string
	PathInfo   string
}

// ResolveFile is the Go port of FileHandler#set_filename: it walks pathInfo
// segment by segment from root, descending into directories, and either
// resolves a concrete file (Found, with Filename set), resolves a directory's
// index file, or stops at a directory (Found=false, the dir-listing case the
// host handles). It returns a *Status (NotFound) when a segment names a missing
// file, or when a resolved basename is a nondisclosure / Windows-ambiguous name.
//
// prevent_directory_traversal (File.expand_path on path_info) and the file read
// are host seams; pathInfo is expected already normalised (the request path is
// NormalizePath'd, and ".." cannot survive that).
func (h *FileHandler) ResolveFile(pathInfo string) (FileResolution, *Status) {
	filename := h.root
	scriptName := ""

	segments := scanSegments(pathInfo)
	// path_info.unshift("") — dummy for the @root dir check.
	segments = append([]string{""}, segments...)

	// while base = path_info.first ... descend through directories.
	for len(segments) > 0 {
		base := segments[0]
		if base == "/" {
			break
		}
		if !h.fs.IsDir(expandPath(filename + base)) {
			break
		}
		// shift_path_info
		consumed := segments[0]
		segments = segments[1:]
		scriptName += consumed
		filename = expandPath(filename + consumed)
		if st := h.checkFilename(path.Base(filename)); st != nil {
			return FileResolution{}, st
		}
	}

	if len(segments) > 0 {
		base := segments[0]
		if base == "/" {
			if file, found := h.searchIndexFile(filename); found {
				// shift_path_info with the found index file as base
				segments = segments[1:]
				scriptName += file
				filename = expandPath(filename + file)
				if st := h.checkFilename(path.Base(filename)); st != nil {
					return FileResolution{}, st
				}
				return FileResolution{
					Found: true, Filename: filename,
					ScriptName: scriptName, PathInfo: strings.Join(segments, ""),
				}, nil
			}
			// shift_path_info (consume the trailing "/"). expandPath(filename+"/")
			// leaves the basename unchanged from the directory already validated
			// in the descent walk, so no further checkFilename is needed here.
			consumed := segments[0]
			segments = segments[1:]
			scriptName += consumed
			filename = expandPath(filename + consumed)
		} else if file, found := h.searchFile(filename, base); found {
			segments = segments[1:]
			scriptName += file
			filename = expandPath(filename + file)
			if st := h.checkFilename(path.Base(filename)); st != nil {
				return FileResolution{}, st
			}
			return FileResolution{
				Found: true, Filename: filename,
				ScriptName: scriptName, PathInfo: strings.Join(segments, ""),
			}, nil
		} else {
			return FileResolution{}, StatusNotFound("'" + pathInfo + "' not found.")
		}
	}

	// Fell through to a directory: not a file (the host does the dir listing).
	return FileResolution{
		Found: false, Filename: filename,
		ScriptName: scriptName, PathInfo: strings.Join(segments, ""),
	}, nil
}

// searchIndexFile ports search_index_file: try each DirectoryIndex name.
func (h *FileHandler) searchIndexFile(dir string) (string, bool) {
	for _, index := range h.options.DirectoryIndex {
		if file, ok := h.searchFile(dir, "/"+index); ok {
			return file, true
		}
	}
	return "", false
}

// searchFile ports search_file (without the AcceptableLanguages variants, which
// need accept-language negotiation rarely used and content the host provides):
// the basename resolves when root+basename is a regular file.
func (h *FileHandler) searchFile(dir, basename string) (string, bool) {
	if h.fs.IsFile(dir + basename) {
		return basename, true
	}
	return "", false
}

// checkFilename ports check_filename: a nondisclosure or Windows-ambiguous
// basename is NotFound.
func (h *FileHandler) checkFilename(name string) *Status {
	if h.nondisclosureName(name) || windowsAmbiguousName(name) {
		return StatusNotFound("'" + name + "' not found.")
	}
	return nil
}

// nondisclosureName ports nondisclosure_name?: a name matching any
// NondisclosureName glob (case-insensitive) is hidden.
func (h *FileHandler) nondisclosureName(name string) bool {
	for _, pattern := range h.options.NondisclosureName {
		if fnmatchCasefold(pattern, name) {
			return true
		}
	}
	return false
}

// windowsAmbiguousName ports windows_ambiguous_name?: trailing dots/spaces, or a
// "::$DATA" suffix.
func windowsAmbiguousName(name string) bool {
	if name == "" {
		return false
	}
	if last := name[len(name)-1]; last == '.' || last == ' ' {
		return true
	}
	return strings.HasSuffix(name, "::$DATA")
}

// scanSegments ports req.path_info.scan(%r|/[^/]*|): each match is a '/'
// followed by a run of non-slash chars. A path_info that does not start with
// '/' (or is empty) yields no segments.
func scanSegments(pathInfo string) []string {
	var out []string
	i := 0
	for i < len(pathInfo) {
		if pathInfo[i] != '/' {
			i++
			continue
		}
		j := i + 1
		for j < len(pathInfo) && pathInfo[j] != '/' {
			j++
		}
		out = append(out, pathInfo[i:j])
		i = j
	}
	return out
}

// expandPath collapses a path the way File.expand_path does for the already
// '/'-joined, '..'-free paths FileHandler builds: it cleans redundant slashes
// and '.' but keeps the result absolute. Since the request path is normalised
// (no ".."), path.Clean is faithful here.
func expandPath(p string) string {
	cleaned := path.Clean(p)
	return cleaned
}

// fnmatchCasefold implements File.fnmatch(pattern, name, FNM_CASEFOLD) for the
// glob features WEBrick's NondisclosureName uses: '*' (any run, including none),
// '?' (one char), and literal chars, all case-insensitive. It does not handle
// '/' specially (basenames have no '/').
func fnmatchCasefold(pattern, name string) bool {
	return globMatch(strings.ToLower(pattern), strings.ToLower(name))
}

// globMatch is a backtrack-free '*'/'?' glob matcher.
func globMatch(pattern, s string) bool {
	pi, si := 0, 0
	star, match := -1, 0
	for si < len(s) {
		switch {
		case pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == s[si]):
			pi++
			si++
		case pi < len(pattern) && pattern[pi] == '*':
			star = pi
			match = si
			pi++
		case star != -1:
			pi = star + 1
			match++
			si = match
		default:
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}
