// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

// VERSION is the WEBrick version this port mirrors (WEBrick::VERSION).
const VERSION = "1.9.2"

// Config is the pure-Go port of the WEBrick::Config::HTTP defaults relevant to
// the request/response/servlet core. Networking-only keys (BindAddress, Port
// listen socket, MaxClients, the various callbacks, SSL) are host-side seams and
// are not modelled here; the fields below are the ones the codec actually reads.
type Config struct {
	// Port is the server's logical port (WEBrick::Config::HTTP[:Port], default
	// 80), used to default the request URI port and in error-page addresses.
	Port int
	// ServerName is the default host (Utils.getservername); used when neither a
	// Host header nor a socket address supplies one.
	ServerName string
	// ServerSoftware is the Server header / error-page signature.
	ServerSoftware string
	// HTTPVersion is the server's protocol version (default 1.1).
	HTTPVersion HTTPVersion
	// MimeTypes maps a lowercased file suffix to a MIME type (DefaultMimeTypes).
	MimeTypes map[string]string
	// DirectoryIndex is the FileHandler index-file search order.
	DirectoryIndex []string
	// Escape8bitURI mirrors :Escape8bitURI; when set the unparsed URI is
	// escape8bit'd before parsing.
	Escape8bitURI bool
}

// DefaultConfig returns the WEBrick::Config::HTTP defaults for the keys this
// library reads. ServerName defaults to "" (the host fills it, as MRI does via
// Utils.getservername at runtime); ServerSoftware mirrors the
// "WEBrick/<ver> (Ruby/<ver>/<date>)" template with the version known here.
func DefaultConfig() *Config {
	return &Config{
		Port:           80,
		ServerName:     "",
		ServerSoftware: "WEBrick/" + VERSION,
		HTTPVersion:    HTTPVersion{Major: 1, Minor: 1},
		MimeTypes:      DefaultMimeTypes,
		DirectoryIndex: []string{"index.html", "index.htm", "index.cgi", "index.rhtml"},
		Escape8bitURI:  false,
	}
}

// DefaultMimeTypes is the exact WEBrick::HTTPUtils::DefaultMimeTypes table.
var DefaultMimeTypes = map[string]string{
	"ai":          "application/postscript",
	"asc":         "text/plain",
	"avi":         "video/x-msvideo",
	"avif":        "image/avif",
	"bin":         "application/octet-stream",
	"bmp":         "image/bmp",
	"class":       "application/octet-stream",
	"cer":         "application/pkix-cert",
	"crl":         "application/pkix-crl",
	"crt":         "application/x-x509-ca-cert",
	"css":         "text/css",
	"dms":         "application/octet-stream",
	"doc":         "application/msword",
	"dvi":         "application/x-dvi",
	"eps":         "application/postscript",
	"etx":         "text/x-setext",
	"exe":         "application/octet-stream",
	"gif":         "image/gif",
	"htm":         "text/html",
	"html":        "text/html",
	"ico":         "image/x-icon",
	"jpe":         "image/jpeg",
	"jpeg":        "image/jpeg",
	"jpg":         "image/jpeg",
	"js":          "application/javascript",
	"json":        "application/json",
	"lha":         "application/octet-stream",
	"lzh":         "application/octet-stream",
	"mjs":         "application/javascript",
	"mov":         "video/quicktime",
	"mp4":         "video/mp4",
	"mpe":         "video/mpeg",
	"mpeg":        "video/mpeg",
	"mpg":         "video/mpeg",
	"otf":         "font/otf",
	"pbm":         "image/x-portable-bitmap",
	"pdf":         "application/pdf",
	"pgm":         "image/x-portable-graymap",
	"png":         "image/png",
	"pnm":         "image/x-portable-anymap",
	"ppm":         "image/x-portable-pixmap",
	"ppt":         "application/vnd.ms-powerpoint",
	"ps":          "application/postscript",
	"qt":          "video/quicktime",
	"ras":         "image/x-cmu-raster",
	"rb":          "text/plain",
	"rd":          "text/plain",
	"rtf":         "application/rtf",
	"sgm":         "text/sgml",
	"sgml":        "text/sgml",
	"svg":         "image/svg+xml",
	"tif":         "image/tiff",
	"tiff":        "image/tiff",
	"ttc":         "font/collection",
	"ttf":         "font/ttf",
	"txt":         "text/plain",
	"wasm":        "application/wasm",
	"webm":        "video/webm",
	"webmanifest": "application/manifest+json",
	"webp":        "image/webp",
	"woff":        "font/woff",
	"woff2":       "font/woff2",
	"xbm":         "image/x-xbitmap",
	"xhtml":       "text/html",
	"xls":         "application/vnd.ms-excel",
	"xml":         "text/xml",
	"xpm":         "image/x-xpixmap",
	"xwd":         "image/x-xwindowdump",
	"zip":         "application/zip",
}
