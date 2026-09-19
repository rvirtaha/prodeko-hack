// Package mcpserver is the MCP streamable-HTTP transport: JSON-RPC 2.0 over
// POST /mcp, one request or a batch of them per call.
//
// It implements the half of the transport this server needs and no more.
// There is no server-initiated stream, so GET /mcp is refused: nothing here
// pushes notifications, and a client that cannot fall back to POST-only is
// better off told that immediately.
//
// The package knows nothing about the site, git or the fence. It authenticates
// a bearer token through a function the caller supplies, and dispatches
// tools/call to a [Tool] the caller registered.
package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ProtocolVersion is the MCP revision this transport speaks. It is reported in
// initialize and is the only version negotiated.
const ProtocolVersion = "2025-06-18"

// Path is where the transport mounts. It is part of the published resource
// identifier, so it is fixed here rather than configured.
const Path = "/mcp"

// DefaultMaxBodyBytes bounds one JSON-RPC request body. The largest legitimate
// one is a write_file of a 2 MB text file, JSON-escaped.
const DefaultMaxBodyBytes = 8 << 20

// Identity is who the caller is, as the bearer token proved. It is the whole
// of what a tool learns about them: the fence and the branch namespace are
// derived from Username, and commits are authored as Name <Email>.
//
// It carries no roles. The role conjunction is checked once, at sign-in, by
// whoever mints the token.
type Identity struct {
	Username string
	Name     string
	Email    string
}

// Tool is one callable. Call returns the text the model reads; an error from
// it is reported to the client as a failed tool result, not as a transport
// error, because a refusal is something the model should read and act on.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage // JSON Schema for the arguments object
	Call        func(ctx context.Context, id Identity, args json.RawMessage) (string, error)
}

// Authenticator turns the bearer token of a request into an identity. An
// error, of any kind, is a 401; the reason is logged and never returned.
type Authenticator func(bearer string) (Identity, error)

// ErrUnauthorized is what an Authenticator returns for a token it does not
// accept. Any other error is treated the same way.
var ErrUnauthorized = errors.New("mcpserver: bearer token not accepted")

type Config struct {
	// Name and Version are the serverInfo reported in initialize.
	Name    string
	Version string

	// Instructions ride in initialize. They are where the site conventions are
	// told to the model once per session: the two content roots, the
	// translationKey pairing, tokens over hex, the Finnish page's English pair.
	Instructions string

	Tools        []Tool
	Authenticate Authenticator

	// ResourceMetadataURL is the RFC 9728 document a 401 points at, so an MCP
	// client that has never seen this server can discover how to sign in.
	ResourceMetadataURL string

	MaxBodyBytes int64        // zero means DefaultMaxBodyBytes
	Logger       *slog.Logger // nil means slog.Default
}

// Server is the transport. It is safe for concurrent use; the tool set is
// fixed at construction.
type Server struct {
	cfg   Config
	tools map[string]Tool
	order []string
	log   *slog.Logger
}

// New checks the tool set: names are unique and every schema is a JSON object,
// because a client that cannot parse inputSchema fails a whole session rather
// than one call.
func New(cfg Config) (*Server, error) {
	if cfg.Authenticate == nil {
		return nil, errors.New("mcpserver: an Authenticator is required; an unauthenticated /mcp is a write credential on the open internet")
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, errors.New("mcpserver: Name must be set")
	}
	if cfg.MaxBodyBytes == 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	s := &Server{
		cfg:   cfg,
		tools: make(map[string]Tool, len(cfg.Tools)),
		order: make([]string, 0, len(cfg.Tools)),
		log:   cfg.Logger,
	}
	for _, t := range cfg.Tools {
		if t.Name == "" {
			return nil, errors.New("mcpserver: a tool has no name")
		}
		if _, dup := s.tools[t.Name]; dup {
			return nil, fmt.Errorf("mcpserver: tool %q is registered twice", t.Name)
		}
		if t.Call == nil {
			return nil, fmt.Errorf("mcpserver: tool %q has no Call", t.Name)
		}
		var probe map[string]any
		if err := json.Unmarshal(t.Schema, &probe); err != nil {
			return nil, fmt.Errorf("mcpserver: tool %q has no usable JSON Schema: %w", t.Name, err)
		}
		s.tools[t.Name] = t
		s.order = append(s.order, t.Name)
	}
	return s, nil
}

// Register mounts the transport on mux behind bearer authentication.
func (s *Server) Register(mux *http.ServeMux) {
	mux.Handle(Path, s.Handler())
}

// Handler is the transport with its authentication middleware wrapped around
// it. Nothing below it runs for a request that did not authenticate.
func (s *Server) Handler() http.Handler {
	return RequireBearer(s.cfg.Authenticate, s.cfg.ResourceMetadataURL, s.log, http.HandlerFunc(s.serve))
}

// ServeHTTP is the transport without authentication. Callers that mount this
// directly are responsible for putting [RequireBearer] in front of it.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.serve(w, r) }

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// No server-initiated stream, so no SSE channel to open.
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "this MCP endpoint speaks POST only", http.StatusMethodNotAllowed)
		return
	}
	id, ok := IdentityFrom(r.Context())
	if !ok {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	// The MCP-Protocol-Version header is read by nothing here: no answer varies
	// by revision, so refusing an unfamiliar value would break a client for no
	// gain. The version the client asked for is logged at initialize instead.
	s.handleRPC(w, r, id)
}

// handleRPC reads one JSON-RPC request or batch and answers it. A batch that
// is all notifications is answered 202 with no body.
//
// A body that does not parse is an HTTP 400 carrying a JSON-RPC error with a
// null id; everything that parses is an HTTP 200, because once a request is
// readable its outcome belongs in the JSON-RPC envelope where the client's
// per-request machinery can see it.
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request, id Identity) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.write(w, http.StatusRequestEntityTooLarge, errorResponse(nullID, codeInvalidRequest,
				"the request body is too large",
				fmt.Sprintf("the limit is %d bytes; write_file is the tool with a size limit of its own", s.cfg.MaxBodyBytes)))
			return
		}
		s.write(w, http.StatusBadRequest, errorResponse(nullID, codeInvalidRequest, "the request body could not be read", err.Error()))
		return
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		s.write(w, http.StatusBadRequest, errorResponse(nullID, codeParseError, "the request body is empty",
			"expected one JSON-RPC message or an array of them"))
		return
	}

	ctx := r.Context()

	// The batch form is accepted although this protocol revision no longer
	// requires it: clients built against earlier revisions still send one, and
	// answering it costs a loop.
	if trimmed[0] != '[' {
		var req request
		if err := json.Unmarshal(trimmed, &req); err != nil {
			s.write(w, http.StatusBadRequest, errorResponse(nullID, codeParseError, "the request body is not a JSON-RPC message", err.Error()))
			return
		}
		resp := s.dispatch(ctx, id, req)
		if resp == nil {
			s.accepted(w)
			return
		}
		s.write(w, http.StatusOK, resp)
		return
	}

	var messages []json.RawMessage
	if err := json.Unmarshal(trimmed, &messages); err != nil {
		s.write(w, http.StatusBadRequest, errorResponse(nullID, codeParseError, "the batch is not valid JSON", err.Error()))
		return
	}
	if len(messages) == 0 {
		s.write(w, http.StatusBadRequest, errorResponse(nullID, codeInvalidRequest, "an empty batch is not a request", nil))
		return
	}

	// One unreadable element costs its own answer and nothing more.
	answers := make([]*response, 0, len(messages))
	for _, msg := range messages {
		var req request
		if err := json.Unmarshal(msg, &req); err != nil {
			answers = append(answers, errorResponse(nullID, codeParseError, "this message is not a JSON-RPC object", err.Error()))
			continue
		}
		if resp := s.dispatch(ctx, id, req); resp != nil {
			answers = append(answers, resp)
		}
	}
	if len(answers) == 0 {
		s.accepted(w)
		return
	}
	s.write(w, http.StatusOK, answers)
}

// accepted answers a body that was all notifications. There is nothing to reply
// with, and an empty array is not a legal JSON-RPC body.
func (s *Server) accepted(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
}

// dispatch answers one request. It returns nil for a notification, which has
// no reply by definition.
func (s *Server) dispatch(ctx context.Context, id Identity, req request) *response {
	if req.isNotification() {
		// notifications/initialized ends the handshake and notifications/
		// cancelled asks for work this transport cannot interrupt. Both are
		// acknowledged by the 202 the caller writes, and neither may be
		// answered with an error: a notification has no id to answer to.
		s.log.Debug("mcp notification", "method", req.Method, "user", id.Username)
		return nil
	}
	if req.hasNullID() {
		return errorResponse(nullID, codeInvalidRequest, "a request id must not be null", req.Method)
	}
	if req.JSONRPC != jsonrpcVersion {
		return errorResponse(req.ID, codeInvalidRequest, `the "jsonrpc" member must be "2.0"`, req.JSONRPC)
	}
	if req.Method == "" {
		return errorResponse(req.ID, codeInvalidRequest, "this message has no method",
			"the server sends no requests, so it expects no responses")
	}

	switch req.Method {
	case methodInitialize:
		return s.initialize(req)
	case methodPing:
		return resultResponse(req.ID, emptyResult{})
	case methodToolsList:
		return s.listTools(req)
	case methodToolsCall:
		return s.callTool(ctx, id, req)
	default:
		// resources/list and prompts/list land here by design: the capabilities
		// advertise tools only, and a client that asks anyway is told so.
		return errorResponse(req.ID, codeMethodNotFound, "unknown method "+strconv.Quote(req.Method),
			unknownMethodData{Method: req.Method, Supported: supportedMethods})
	}
}

// initialize answers the handshake. One protocol version is spoken; a client
// that asked for another is told ours and decides for itself whether to go on.
func (s *Server) initialize(req request) *response {
	var p initializeParams
	if len(bytes.TrimSpace(req.Params)) != 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, codeInvalidParams, "the initialize params could not be read", err.Error())
		}
	}
	s.log.Info("mcp initialize",
		"client", p.ClientInfo.Name, "client_version", p.ClientInfo.Version,
		"asked", p.ProtocolVersion, "speaking", ProtocolVersion)

	return resultResponse(req.ID, initializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    capabilities{Tools: &toolsCapability{}},
		ServerInfo:      serverInfo{Name: s.cfg.Name, Version: s.cfg.Version},
		Instructions:    s.cfg.Instructions,
	})
}

// listTools returns the whole set. Nine tools fit in one answer, so there is no
// cursor and a cursor the client invents is ignored rather than refused.
func (s *Server) listTools(req request) *response {
	tools := make([]toolDescriptor, 0, len(s.order))
	for _, name := range s.order {
		t := s.tools[name]
		tools = append(tools, toolDescriptor{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Schema,
		})
	}
	return resultResponse(req.ID, toolsListResult{Tools: tools})
}

// callTool dispatches to one tool. Everything the tool itself reports — a path
// outside the fence, a build that failed, an edit that matched twice — comes
// back as an ordinary result with isError set, because the model is meant to
// read it and try again. Only the call being unanswerable at all, an unknown
// name or unreadable params, is a protocol error.
func (s *Server) callTool(ctx context.Context, id Identity, req request) *response {
	var p callToolParams
	if len(bytes.TrimSpace(req.Params)) != 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, codeInvalidParams, "the tools/call params could not be read", err.Error())
		}
	}
	if p.Name == "" {
		return errorResponse(req.ID, codeInvalidParams, "tools/call needs a tool name",
			unknownToolData{Known: s.order})
	}
	tool, ok := s.tools[p.Name]
	if !ok {
		return errorResponse(req.ID, codeInvalidParams, "unknown tool "+strconv.Quote(p.Name),
			unknownToolData{Tool: p.Name, Known: s.order})
	}

	args := json.RawMessage(bytes.TrimSpace(p.Arguments))
	switch {
	case len(args) == 0 || bytes.Equal(args, []byte("null")):
		// Clients differ on whether a tool that takes no arguments gets an
		// empty object or nothing at all. Every tool decodes an object.
		args = json.RawMessage(`{}`)
	case args[0] != '{':
		return errorResponse(req.ID, codeInvalidParams, "the tool arguments must be a JSON object",
			fmt.Sprintf("%s was given %s", p.Name, string(args[:1])))
	}

	started := time.Now()
	out, err := s.invoke(ctx, tool, id, args)
	if err != nil {
		s.log.Info("tool refused", "tool", tool.Name, "user", id.Username, "err", err, "took", time.Since(started))
		return resultResponse(req.ID, textResult(err.Error(), true))
	}
	s.log.Info("tool ran", "tool", tool.Name, "user", id.Username, "bytes", len(out), "took", time.Since(started))
	return resultResponse(req.ID, textResult(out, false))
}

// invoke runs one tool, turning a panic into an ordinary error. A bug in one
// tool must not drop the answers to the rest of a batch, and the model reading
// the result is better served by a sentence than by a closed connection.
func (s *Server) invoke(ctx context.Context, t Tool, id Identity, args json.RawMessage) (out string, err error) {
	defer func() {
		if p := recover(); p != nil {
			s.log.Error("tool panicked", "tool", t.Name, "user", id.Username, "panic", p)
			err = fmt.Errorf("the %s tool failed unexpectedly; this is a bug in the server", t.Name)
		}
	}()
	return t.Call(ctx, id, args)
}

// internalErrorBody is the answer to a response that would not encode. It is a
// literal because the encoder is the thing that just failed.
var internalErrorBody = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":` +
	strconv.Itoa(codeInternalError) + `,"message":"the response could not be encoded"}}`)

// write encodes payload and sends it. The encoding happens first so a payload
// that will not marshal cannot leave a half-written 200 behind.
func (s *Server) write(w http.ResponseWriter, status int, payload any) {
	buf, err := json.Marshal(payload)
	if err != nil {
		s.log.Error("mcp response could not be encoded", "err", err)
		buf, status = internalErrorBody, http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if _, err := w.Write(buf); err != nil {
		s.log.Debug("mcp response could not be written", "err", err)
	}
}

// Tools is the registered tool set, in registration order.
func (s *Server) Tools() []Tool {
	out := make([]Tool, 0, len(s.order))
	for _, name := range s.order {
		out = append(out, s.tools[name])
	}
	return out
}
