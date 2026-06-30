// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import (
	"sort"
	"strings"
)

// Servlet is the Go port of the WEBrick::HTTPServlet::AbstractServlet service
// contract: Service dispatches a request to the do_<METHOD> handler. A servlet
// raising a *Status returns it as the error (the server turns it into the
// response). The default handlers (do_GET -> NotFound, do_HEAD -> do_GET,
// do_OPTIONS -> Allow list) are provided by AbstractServlet which concrete
// servlets embed.
type Servlet interface {
	// Service dispatches req to the matching do_<METHOD>, mirroring
	// AbstractServlet#service. It returns a *Status to "raise" (e.g.
	// MethodNotAllowed for an unhandled method), or nil on success.
	Service(req *Request, res *Response) *Status
}

// HandlerFunc is a do_<METHOD> implementation.
type HandlerFunc func(req *Request, res *Response) *Status

// AbstractServlet is the Go port of WEBrick::HTTPServlet::AbstractServlet: it
// holds the per-method handler table and implements the service dispatch
// (do_<METHOD>, with HEAD aliased to GET and a default OPTIONS that reports the
// Allow list). Concrete servlets register handlers via Handle.
type AbstractServlet struct {
	handlers map[string]HandlerFunc
}

// NewAbstractServlet returns an AbstractServlet with WEBrick's default GET
// (NotFound), HEAD (-> GET) and OPTIONS (Allow) handlers installed.
func NewAbstractServlet() *AbstractServlet {
	s := &AbstractServlet{handlers: map[string]HandlerFunc{}}
	s.handlers["GET"] = func(req *Request, res *Response) *Status {
		return StatusNotFound("not found.")
	}
	return s
}

// Handle registers fn as the do_<method> handler (method uppercased, '-' kept
// as '_' in MRI's method name but here keyed by the raw uppercase method).
func (s *AbstractServlet) Handle(method string, fn HandlerFunc) {
	s.handlers[strings.ToUpper(method)] = fn
}

// allowList returns the sorted methods this servlet handles (do_OPTIONS's
// Allow). HEAD is implicit when GET is handled, matching the do_HEAD alias.
func (s *AbstractServlet) allowList() string {
	set := map[string]bool{}
	for m := range s.handlers {
		set[m] = true
	}
	if set["GET"] {
		set["HEAD"] = true
	}
	set["OPTIONS"] = true
	methods := make([]string, 0, len(set))
	for m := range set {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	return strings.Join(methods, ",")
}

// Service dispatches req to the registered handler, mirroring
// AbstractServlet#service: HEAD falls through to GET; OPTIONS defaults to the
// Allow list; an unhandled method raises MethodNotAllowed.
func (s *AbstractServlet) Service(req *Request, res *Response) *Status {
	method := req.RequestMethod
	if fn, ok := s.handlers[method]; ok {
		return fn(req, res)
	}
	if method == "HEAD" {
		if fn, ok := s.handlers["GET"]; ok {
			return fn(req, res)
		}
	}
	if method == "OPTIONS" {
		res.Set("allow", s.allowList())
		return nil
	}
	return StatusMethodNotAllowed("unsupported method '" + req.RequestMethod + "'.")
}

// ProcServlet is the Go port of WEBrick::HTTPServlet::ProcHandler: a single proc
// mounted via mount_proc, dispatched for GET, POST and PUT (the do_GET alias
// chain). Other methods fall through to AbstractServlet (MethodNotAllowed /
// OPTIONS).
type ProcServlet struct {
	proc HandlerFunc
}

// NewProcServlet wraps proc as a ProcHandler.
func NewProcServlet(proc HandlerFunc) *ProcServlet { return &ProcServlet{proc: proc} }

// Service dispatches GET/POST/PUT to the proc, mirroring ProcHandler's
// do_GET/do_POST/do_PUT aliases; HEAD maps to the proc too (via do_GET);
// OPTIONS reports Allow; anything else is MethodNotAllowed.
func (p *ProcServlet) Service(req *Request, res *Response) *Status {
	switch req.RequestMethod {
	case "GET", "POST", "PUT", "HEAD":
		return p.proc(req, res)
	case "OPTIONS":
		res.Set("allow", "GET,HEAD,OPTIONS,POST,PUT")
		return nil
	default:
		return StatusMethodNotAllowed("unsupported method '" + req.RequestMethod + "'.")
	}
}
