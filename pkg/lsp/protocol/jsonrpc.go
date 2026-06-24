package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

const (
	ErrorCodeParseError     = -32700
	ErrorCodeInvalidRequest = -32600
	ErrorCodeMethodNotFound = -32601
	ErrorCodeInvalidParams  = -32602
	ErrorCodeInternalError  = -32603
	ErrorCodeRequestCancel  = -32800
)

type idKind int

const (
	idKindUnset idKind = iota
	idKindNumber
	idKindString
)

// ID is a JSON-RPC request id.
type ID struct {
	kind   idKind
	number json.Number
	text   string
}

// NewNumberID returns a numeric JSON-RPC request id.
func NewNumberID(n int64) *ID {
	return &ID{kind: idKindNumber, number: json.Number(strconv.FormatInt(n, 10))}
}

// NewStringID returns a string JSON-RPC request id.
func NewStringID(s string) *ID {
	return &ID{kind: idKindString, text: s}
}

// Number returns the id as a number when it was encoded as a number.
func (id ID) Number() (json.Number, bool) {
	return id.number, id.kind == idKindNumber
}

// String returns the id as a string when it was encoded as a string.
func (id ID) String() (string, bool) {
	return id.text, id.kind == idKindString
}

func (id ID) MarshalJSON() ([]byte, error) {
	switch id.kind {
	case idKindNumber:
		return []byte(id.number.String()), nil
	case idKindString:
		return json.Marshal(id.text)
	default:
		return []byte("null"), nil
	}
}

func (id *ID) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		id.kind = idKindString
		id.text = text
		return nil
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err == nil {
		id.kind = idKindNumber
		id.number = number
		return nil
	}
	return fmt.Errorf("invalid JSON-RPC id")
}

// RequestMessage is a JSON-RPC request or notification.
type RequestMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *ID             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ResponseMessage is a JSON-RPC response.
type ResponseMessage struct {
	JSONRPC string
	ID      *ID
	Result  any
	Error   *ResponseError
}

func (r ResponseMessage) MarshalJSON() ([]byte, error) {
	jsonrpc := r.JSONRPC
	if jsonrpc == "" {
		jsonrpc = "2.0"
	}
	if r.Error != nil {
		return json.Marshal(struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      *ID            `json:"id"`
			Error   *ResponseError `json:"error"`
		}{
			JSONRPC: jsonrpc,
			ID:      r.ID,
			Error:   r.Error,
		})
	}
	result, err := json.Marshal(r.Result)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      *ID             `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{
		JSONRPC: jsonrpc,
		ID:      r.ID,
		Result:  result,
	})
}

// ResponseError is a JSON-RPC response error object.
type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// MethodNotFound returns the standard JSON-RPC method-not-found error.
func MethodNotFound(method string) *ResponseError {
	return &ResponseError{
		Code:    ErrorCodeMethodNotFound,
		Message: "method not found: " + method,
	}
}

// InvalidParams returns the standard JSON-RPC invalid-params error.
func InvalidParams(message string) *ResponseError {
	return &ResponseError{Code: ErrorCodeInvalidParams, Message: message}
}

// InternalError returns the standard JSON-RPC internal-error object.
func InternalError(err error) *ResponseError {
	return &ResponseError{Code: ErrorCodeInternalError, Message: err.Error()}
}

// Handler serves JSON-RPC requests.
type Handler interface {
	HandleRequest(context.Context, *RequestMessage) (any, *ResponseError)
}

// Stopper reports whether the server should stop reading messages.
type Stopper interface {
	StopRequested() bool
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(context.Context, *RequestMessage) (any, *ResponseError)

// HandleRequest calls f.
func (f HandlerFunc) HandleRequest(ctx context.Context, req *RequestMessage) (any, *ResponseError) {
	return f(ctx, req)
}
