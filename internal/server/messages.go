package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/provider"
	"github.com/dibakshya01/freebie-frog/internal/translate"
)

// maxRequestBytes caps the incoming request body (Anthropic's documented limit is 32 MB).
const maxRequestBytes = 32 << 20

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if s.deps.Dispatcher == nil {
		writeAnthropicError(w, http.StatusServiceUnavailable, "api_error", "no providers configured; set up config.json (see README)")
		return
	}
	req, ok := s.decodeRequest(w, r)
	if !ok {
		return
	}

	if req.Stream {
		s.streamMessages(w, r, req)
		return
	}

	resp, err := s.deps.Dispatcher.Complete(r.Context(), req)
	if err != nil {
		pe := asProviderError(err)
		writeAnthropicError(w, pe.StatusCode, pe.Type, pe.Message)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) streamMessages(w http.ResponseWriter, r *http.Request, req *anthropic.Request) {
	sw := anthropic.NewSSEWriter(w)
	emitted, err := s.deps.Dispatcher.Stream(r.Context(), req, sw.Emit)
	if err == nil {
		return
	}
	if emitted {
		// Already streaming to the client (an error event was sent, or the client
		// disconnected). Nothing more we can send; just log.
		s.log.Warn("stream ended with error after first event", "error", err.Error())
		return
	}
	// Nothing was written yet: send a normal JSON error with the right status.
	pe := asProviderError(err)
	writeAnthropicError(w, pe.StatusCode, pe.Type, pe.Message)
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	req, ok := s.decodeRequest(w, r)
	if !ok {
		return
	}
	// Approximate — providers rarely expose exact prompt tokens (see CAPABILITIES.md).
	writeJSON(w, http.StatusOK, map[string]int{"input_tokens": translate.EstimateRequestTokens(req)})
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	type model struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	data := []model{}
	if s.deps.Registry != nil {
		for _, id := range s.deps.Registry.AdvertisedModels() {
			data = append(data, model{Type: "model", ID: id})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// decodeRequest reads and validates an Anthropic request body. On error it writes an
// Anthropic error response and returns ok=false.
func (s *Server) decodeRequest(w http.ResponseWriter, r *http.Request) (*anthropic.Request, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes))
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "could not read request body")
		return nil, false
	}
	var req anthropic.Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
		return nil, false
	}
	if req.Model == "" {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "field 'model' is required")
		return nil, false
	}
	if len(req.Messages) == 0 {
		writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "field 'messages' must not be empty")
		return nil, false
	}
	return &req, true
}

func writeAnthropicError(w http.ResponseWriter, status int, etype, msg string) {
	writeJSON(w, status, anthropic.NewError(etype, msg))
}

func asProviderError(err error) *provider.Error {
	if pe, ok := err.(*provider.Error); ok {
		return pe
	}
	return &provider.Error{StatusCode: http.StatusInternalServerError, Type: "api_error", Message: err.Error()}
}
