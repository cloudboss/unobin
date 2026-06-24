package protocol

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
)

// Sender writes a server-to-client notification.
type Sender func(method string, params any) error

// SenderSetter accepts a server-to-client notification sender.
type SenderSetter interface {
	SetSender(Sender)
}

// ServerOptions configures JSON-RPC tracing and logging.
type ServerOptions struct {
	Trace io.Writer
	Log   io.Writer
}

// Server serves JSON-RPC messages over LSP stdio framing.
type Server struct {
	reader  *bufio.Reader
	writer  io.Writer
	handler Handler
	trace   io.Writer
	logger  *log.Logger
}

// NewServer returns a JSON-RPC server using LSP stdio framing.
func NewServer(r io.Reader, w io.Writer, handler Handler) *Server {
	return NewServerWithOptions(r, w, handler, ServerOptions{})
}

// NewServerWithOptions returns a JSON-RPC server with tracing and logging.
func NewServerWithOptions(
	r io.Reader,
	w io.Writer,
	handler Handler,
	options ServerOptions,
) *Server {
	server := &Server{
		reader:  bufio.NewReader(r),
		writer:  w,
		handler: handler,
		trace:   options.Trace,
	}
	if options.Log != nil {
		server.logger = log.New(options.Log, "", 0)
	}
	if setter, ok := handler.(SenderSetter); ok {
		setter.SetSender(server.Notify)
	}
	return server
}

// Notify writes a server-to-client JSON-RPC notification.
func (s *Server) Notify(method string, params any) error {
	body, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return err
	}
	return s.writeMessage(body)
}

// Serve reads requests until the input stream ends.
func (s *Server) Serve(ctx context.Context) error {
	s.logf("server started")
	defer s.logf("server stopped")
	for {
		body, err := ReadMessage(s.reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := s.traceMessage("in", body); err != nil {
			return err
		}
		if err := s.handleMessage(ctx, body); err != nil {
			return err
		}
		if s.stopRequested() {
			return nil
		}
	}
}

func (s *Server) stopRequested() bool {
	stopper, ok := s.handler.(Stopper)
	return ok && stopper.StopRequested()
}

func (s *Server) handleMessage(ctx context.Context, body []byte) error {
	var req RequestMessage
	if err := json.Unmarshal(body, &req); err != nil {
		s.logf("parse JSON-RPC message: %v", err)
		return s.writeResponse(ResponseMessage{
			JSONRPC: "2.0",
			Error: &ResponseError{
				Code:    ErrorCodeParseError,
				Message: fmt.Sprintf("parse JSON-RPC message: %v", err),
			},
		})
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		s.logf("invalid JSON-RPC request")
		return s.writeResponse(ResponseMessage{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &ResponseError{
				Code:    ErrorCodeInvalidRequest,
				Message: "invalid JSON-RPC request",
			},
		})
	}

	result, rpcErr := s.handler.HandleRequest(ctx, &req)
	if req.ID == nil {
		return nil
	}
	response := ResponseMessage{JSONRPC: "2.0", ID: req.ID}
	if rpcErr != nil {
		s.logf("%s failed: %s", req.Method, rpcErr.Message)
		response.Error = rpcErr
	} else {
		response.Result = result
	}
	return s.writeResponse(response)
}

func (s *Server) writeResponse(response ResponseMessage) error {
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return s.writeMessage(body)
}

func (s *Server) writeMessage(body []byte) error {
	if err := s.traceMessage("out", body); err != nil {
		return err
	}
	return WriteMessage(s.writer, body)
}

func (s *Server) traceMessage(direction string, body []byte) error {
	if s.trace == nil {
		return nil
	}
	entry := struct {
		Direction string          `json:"direction"`
		Message   json.RawMessage `json:"message,omitempty"`
		Raw       string          `json:"raw,omitempty"`
	}{Direction: direction}
	if json.Valid(body) {
		entry.Message = append(json.RawMessage(nil), body...)
	} else {
		entry.Raw = string(body)
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if _, err := s.trace.Write(encoded); err != nil {
		return fmt.Errorf("write JSON-RPC trace: %w", err)
	}
	return nil
}

func (s *Server) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}
