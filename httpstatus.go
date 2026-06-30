// Copyright (c) the go-ruby-webrick/webrick authors
//
// SPDX-License-Identifier: BSD-3-Clause

package webrick

import "strconv"

// This file is the Go port of WEBrick::HTTPStatus (httpstatus.rb): the
// status-code -> reason-phrase table, the per-range category hierarchy
// (Info / Success / Redirect / ClientError / ServerError), and the
// HTTPStatus::Status exception family (NotFound / Forbidden / ...).

// StatusCategory names the HTTPStatus class-hierarchy node a code belongs to,
// matching the WEBrick::HTTPStatus parent classes.
type StatusCategory int

const (
	// CategoryInfo is WEBrick::HTTPStatus::Info (1xx).
	CategoryInfo StatusCategory = iota
	// CategorySuccess is WEBrick::HTTPStatus::Success (2xx).
	CategorySuccess
	// CategoryRedirect is WEBrick::HTTPStatus::Redirect (3xx).
	CategoryRedirect
	// CategoryClientError is WEBrick::HTTPStatus::ClientError (4xx).
	CategoryClientError
	// CategoryServerError is WEBrick::HTTPStatus::ServerError (5xx).
	CategoryServerError
)

// statusMessage is the exact WEBrick::HTTPStatus::StatusMessage table, in MRI's
// declaration order (which is also the CodeToError population order).
var statusMessages = []struct {
	Code    int
	Message string
}{
	{100, "Continue"},
	{101, "Switching Protocols"},
	{200, "OK"},
	{201, "Created"},
	{202, "Accepted"},
	{203, "Non-Authoritative Information"},
	{204, "No Content"},
	{205, "Reset Content"},
	{206, "Partial Content"},
	{207, "Multi-Status"},
	{300, "Multiple Choices"},
	{301, "Moved Permanently"},
	{302, "Found"},
	{303, "See Other"},
	{304, "Not Modified"},
	{305, "Use Proxy"},
	{307, "Temporary Redirect"},
	{400, "Bad Request"},
	{401, "Unauthorized"},
	{402, "Payment Required"},
	{403, "Forbidden"},
	{404, "Not Found"},
	{405, "Method Not Allowed"},
	{406, "Not Acceptable"},
	{407, "Proxy Authentication Required"},
	{408, "Request Timeout"},
	{409, "Conflict"},
	{410, "Gone"},
	{411, "Length Required"},
	{412, "Precondition Failed"},
	{413, "Request Entity Too Large"},
	{414, "Request-URI Too Large"},
	{415, "Unsupported Media Type"},
	{416, "Request Range Not Satisfiable"},
	{417, "Expectation Failed"},
	{422, "Unprocessable Entity"},
	{423, "Locked"},
	{424, "Failed Dependency"},
	{426, "Upgrade Required"},
	{428, "Precondition Required"},
	{429, "Too Many Requests"},
	{431, "Request Header Fields Too Large"},
	{451, "Unavailable For Legal Reasons"},
	{500, "Internal Server Error"},
	{501, "Not Implemented"},
	{502, "Bad Gateway"},
	{503, "Service Unavailable"},
	{504, "Gateway Timeout"},
	{505, "HTTP Version Not Supported"},
	{507, "Insufficient Storage"},
	{511, "Network Authentication Required"},
}

// statusMessageByCode indexes the table by code (WEBrick::HTTPStatus::StatusMessage).
var statusMessageByCode = func() map[int]string {
	m := make(map[int]string, len(statusMessages))
	for _, e := range statusMessages {
		m[e.Code] = e.Message
	}
	return m
}()

// ReasonPhrase returns the reason phrase for code, or "" if the code is not in
// the table (WEBrick::HTTPStatus.reason_phrase: StatusMessage[code], nil-safe).
func ReasonPhrase(code int) string {
	return statusMessageByCode[code]
}

// categoryForCode classifies a code by its hundreds range, matching the
// case 100...200 / 200...300 / ... assignment in httpstatus.rb. The bool is
// false when the code is outside 100..599.
func categoryForCode(code int) (StatusCategory, bool) {
	switch {
	case code >= 100 && code < 200:
		return CategoryInfo, true
	case code >= 200 && code < 300:
		return CategorySuccess, true
	case code >= 300 && code < 400:
		return CategoryRedirect, true
	case code >= 400 && code < 500:
		return CategoryClientError, true
	case code >= 500 && code < 600:
		return CategoryServerError, true
	}
	return 0, false
}

// IsInfo reports a 1xx status (WEBrick::HTTPStatus.info?).
func IsInfo(code int) bool { return code >= 100 && code < 200 }

// IsSuccess reports a 2xx status (WEBrick::HTTPStatus.success?).
func IsSuccess(code int) bool { return code >= 200 && code < 300 }

// IsRedirect reports a 3xx status (WEBrick::HTTPStatus.redirect?).
func IsRedirect(code int) bool { return code >= 300 && code < 400 }

// IsError reports a 4xx or 5xx status (WEBrick::HTTPStatus.error?).
func IsError(code int) bool { return code >= 400 && code < 600 }

// IsClientError reports a 4xx status (WEBrick::HTTPStatus.client_error?).
func IsClientError(code int) bool { return code >= 400 && code < 500 }

// IsServerError reports a 5xx status (WEBrick::HTTPStatus.server_error?).
func IsServerError(code int) bool { return code >= 500 && code < 600 }

// Status is the Go port of a WEBrick::HTTPStatus::Status exception: it carries
// the status code, its frozen reason phrase, and an optional message (the
// `raise HTTPStatus::NotFound, "..."` argument). Servlets "raise" a Status by
// returning it as an error; the server turns it into the response.
type Status struct {
	Code         int
	ReasonPhrase string
	Category     StatusCategory
	Message      string
}

// Error implements the error interface, returning the raised message if any,
// else the reason phrase (matching a bare `raise HTTPStatus::NotFound`).
func (s *Status) Error() string {
	if s.Message != "" {
		return s.Message
	}
	return s.ReasonPhrase
}

// ToI returns the status code (WEBrick::HTTPStatus::Status#to_i / #code).
func (s *Status) ToI() int { return s.Code }

// NewStatus builds the Status exception for code with an optional message,
// mirroring `raise HTTPStatus[code], msg`. It returns nil for a code outside
// the StatusMessage table (CodeToError has no entry).
func NewStatus(code int, message string) *Status {
	msg, ok := statusMessageByCode[code]
	if !ok {
		return nil
	}
	cat, _ := categoryForCode(code)
	return &Status{Code: code, ReasonPhrase: msg, Category: cat, Message: message}
}

// StatusError builds the named status exceptions WEBrick servlets raise. These
// are the constructors for WEBrick::HTTPStatus::NotFound, ::Forbidden, etc.
func StatusError(code int) *Status { return NewStatus(code, "") }

// The named HTTPStatus::Status constructors most servlets reach for, each
// returning the exception with its frozen reason phrase. They take the optional
// raised message exactly like `raise HTTPStatus::NotFound, "..."`.

// StatusOK is HTTPStatus::OK (200).
func StatusOK(msg ...string) *Status { return NewStatus(200, opt(msg)) }

// StatusMovedPermanently is HTTPStatus::MovedPermanently (301).
func StatusMovedPermanently(msg ...string) *Status { return NewStatus(301, opt(msg)) }

// StatusFound is HTTPStatus::Found (302).
func StatusFound(msg ...string) *Status { return NewStatus(302, opt(msg)) }

// StatusTemporaryRedirect is HTTPStatus::TemporaryRedirect (307).
func StatusTemporaryRedirect(msg ...string) *Status { return NewStatus(307, opt(msg)) }

// StatusNotModified is HTTPStatus::NotModified (304).
func StatusNotModified(msg ...string) *Status { return NewStatus(304, opt(msg)) }

// StatusBadRequest is HTTPStatus::BadRequest (400).
func StatusBadRequest(msg ...string) *Status { return NewStatus(400, opt(msg)) }

// StatusForbidden is HTTPStatus::Forbidden (403).
func StatusForbidden(msg ...string) *Status { return NewStatus(403, opt(msg)) }

// StatusNotFound is HTTPStatus::NotFound (404).
func StatusNotFound(msg ...string) *Status { return NewStatus(404, opt(msg)) }

// StatusMethodNotAllowed is HTTPStatus::MethodNotAllowed (405).
func StatusMethodNotAllowed(msg ...string) *Status { return NewStatus(405, opt(msg)) }

// StatusLengthRequired is HTTPStatus::LengthRequired (411).
func StatusLengthRequired(msg ...string) *Status { return NewStatus(411, opt(msg)) }

// StatusRequestEntityTooLarge is HTTPStatus::RequestEntityTooLarge (413).
func StatusRequestEntityTooLarge(msg ...string) *Status { return NewStatus(413, opt(msg)) }

// StatusNotImplemented is HTTPStatus::NotImplemented (501).
func StatusNotImplemented(msg ...string) *Status { return NewStatus(501, opt(msg)) }

// StatusInternalServerError is HTTPStatus::InternalServerError (500).
func StatusInternalServerError(msg ...string) *Status { return NewStatus(500, opt(msg)) }

func opt(msg []string) string {
	if len(msg) > 0 {
		return msg[0]
	}
	return ""
}

// codeString renders a status code as its 3-digit decimal string.
func codeString(code int) string { return strconv.Itoa(code) }
