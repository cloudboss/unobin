package protocol

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// Server serves JSON-RPC messages over LSP stdio framing.
type Server struct {
	reader  *bufio.Reader
	writer  io.Writer
	handler Handler
}

// NewServer returns a JSON-RPC server using LSP stdio framing.
func NewServer(r io.Reader, w io.Writer, handler Handler) *Server {
	return &Server{
		reader:  bufio.NewReader(r),
		writer:  w,
		handler: handler,
	}
}

// Serve reads requests until the input stream ends.
func (s *Server) Serve(ctx context.Context) error {
	for {
		body, err := ReadMessage(s.reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := s.handleMessage(ctx, body); err != nil {
			return err
		}
	}
}

func (s *Server) handleMessage(ctx context.Context, body []byte) error {
	var req RequestMessage
	if err := json.Unmarshal(body, &req); err != nil {
		return s.writeResponse(ResponseMessage{
			JSONRPC: "2.0",
			Error: &ResponseError{
				Code:    ErrorCodeParseError,
				Message: fmt.Sprintf("parse JSON-RPC message: %v", err),
			},
		})
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
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
	return WriteMessage(s.writer, body)
}
