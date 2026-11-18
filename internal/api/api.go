// Package api holds the REST API's JSON types and how answers and errors
// are written. It knows nothing about HTTP routing or the database.
//
// Every error is {"error": {"code", "message", "fields"?, "current"?}}.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/stazelabs/vaquero/internal/core"
)

// Encode is v as indented JSON with a trailing newline.
func Encode(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// WriteJSON writes v with status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	b, err := Encode(v)
	if err != nil {
		WriteFailure(w, Internal())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// Failure is an error answer.
type Failure struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
	Current any               `json:"current,omitempty"`
}

// WriteFailure writes f as {"error": f}.
func WriteFailure(w http.ResponseWriter, f *Failure) {
	b, _ := Encode(map[string]*Failure{"error": f})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.Status)
	_, _ = w.Write(b)
}

// Fail is a Failure with status, code and message.
func Fail(status int, code, message string) *Failure {
	return &Failure{Status: status, Code: code, Message: message}
}

// NotFound is a 404 with message.
func NotFound(message string) *Failure { return Fail(http.StatusNotFound, "not_found", message) }

// Internal is a 500 that says nothing about the cause (log it instead).
func Internal() *Failure {
	return Fail(http.StatusInternalServerError, "server_error", "Something went wrong.")
}

// FromCore turns a core error into its answer: *core.Invalid is 422
// invalid with the field messages, core.ErrNotFound 404, *core.Conflict 409
// with current(conflict) as the current object. Anything else is a 500.
func FromCore(err error, current func(*core.Conflict) any) *Failure {
	var inv *core.Invalid
	var conflict *core.Conflict
	switch {
	case errors.As(err, &inv):
		f := Fail(http.StatusUnprocessableEntity, "invalid", inv.Error())
		f.Fields = inv.Fields
		return f
	case errors.Is(err, core.ErrNotFound):
		return NotFound("That no longer exists.")
	case errors.As(err, &conflict):
		f := Fail(http.StatusConflict, "conflict", conflict.Error())
		if current != nil {
			f.Current = current(conflict)
		}
		return f
	}
	return Internal()
}

// Decode reads one JSON object from the request body into v, refusing
// unknown fields, trailing data and bodies over the server's limit.
func Decode(r *http.Request, v any) *Failure {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		var typ *json.UnmarshalTypeError
		switch {
		case errors.As(err, &tooBig):
			return Fail(http.StatusRequestEntityTooLarge, "too_large", "The request body is too large.")
		case errors.As(err, &typ):
			return Fail(http.StatusBadRequest, "bad_request", fmt.Sprintf("%s should be a %s.", typ.Field, typ.Type))
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			return Fail(http.StatusBadRequest, "bad_request", "Unknown field "+strings.TrimPrefix(err.Error(), "json: unknown field ")+".")
		}
		return Fail(http.StatusBadRequest, "bad_request", "The request body isn't valid JSON.")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Fail(http.StatusBadRequest, "bad_request", "Send one JSON object.")
	}
	return nil
}
