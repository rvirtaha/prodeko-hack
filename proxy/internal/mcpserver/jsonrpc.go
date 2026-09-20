package mcpserver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
)

// jsonrpcVersion is the only value the "jsonrpc" member may carry.
const jsonrpcVersion = "2.0"

// JSON-RPC 2.0 error codes, as MCP uses them. Anything the tools themselves
// report is not one of these: a tool that fails answers with isError on a
// successful call result, because the model is meant to read the failure and
// try again, not to see a transport error.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// The methods this transport answers. The list is data because it is reported
// to a client that called something else.
const (
	methodInitialize    = "initialize"
	methodInitialized   = "notifications/initialized"
	methodPing          = "ping"
	methodToolsList     = "tools/list"
	methodToolsCall     = "tools/call"
	methodPromptsList   = "prompts/list"
	methodPromptsGet    = "prompts/get"
	methodResourcesList = "resources/list"
	methodResourcesRead = "resources/read"
)

var supportedMethods = []string{
	methodInitialize, methodInitialized, methodPing,
	methodToolsList, methodToolsCall,
	methodPromptsList, methodPromptsGet,
	methodResourcesList, methodResourcesRead,
}

// nullID is the id of an error that cannot be attributed to a request: a body
// that did not parse, or one whose id was null. JSON-RPC requires the member to
// be present and null there, so it is spelled out rather than omitted.
var nullID = json.RawMessage("null")

// request is one JSON-RPC 2.0 call. An absent id makes it a notification, and
// a notification is answered with no body at all.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (r request) isNotification() bool { return len(bytes.TrimSpace(r.ID)) == 0 }

// hasNullID reports an explicit "id": null. MCP forbids it, and treating it as
// a notification would silently drop a call the client is waiting on.
func (r request) hasNullID() bool { return bytes.Equal(bytes.TrimSpace(r.ID), []byte("null")) }

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func resultResponse(id json.RawMessage, result any) *response {
	return &response{JSONRPC: jsonrpcVersion, ID: id, Result: result}
}

// errorResponse builds a protocol error. data carries the detail a client needs
// to correct itself: the offending value, or what the server does accept.
func errorResponse(id json.RawMessage, code int, message string, data any) *response {
	if len(bytes.TrimSpace(id)) == 0 {
		id = nullID
	}
	return &response{JSONRPC: jsonrpcVersion, ID: id, Error: &rpcError{Code: code, Message: message, Data: data}}
}

// The result payloads, which are the wire contract with the client.

// initializeParams is what the client sends. Only the version and the name are
// used: this server offers no capability that depends on the client's.
type initializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	ClientInfo      clientInfo `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Capabilities    capabilities `json:"capabilities"`
	ServerInfo      serverInfo   `json:"serverInfo"`
	Instructions    string       `json:"instructions,omitempty"`
}

// capabilities advertises what was configured and nothing else. There is no
// sampling: the model runs on the client's side, and this server is hands and
// guardrails. Prompts and resources appear only when the toolset declared
// some, so a client of a server without them is never invited to ask.
type capabilities struct {
	Tools     *toolsCapability     `json:"tools,omitempty"`
	Prompts   *promptsCapability   `json:"prompts,omitempty"`
	Resources *resourcesCapability `json:"resources,omitempty"`
}

type toolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolsListResult struct {
	Tools []toolDescriptor `json:"tools"`
}

type toolDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Meta        json.RawMessage `json:"_meta,omitempty"` // the MCP Apps ui block, for a tool with a widget
}

// callToolResult carries a tool's answer. A refusal by the fence is an
// ordinary result with IsError set, so the model reads the rule it broke.
type callToolResult struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// content is one block of a tool result: prose, or a base64 PNG. The members of
// the other kind are omitted rather than sent empty, so an image block is an
// image block and nothing else.
//
// Text is a pointer because an empty text block is still a text block: the
// member is required, and omitempty on a string would drop the one thing the
// block has to carry.
type content struct {
	Type     string  `json:"type"`
	Text     *string `json:"text,omitempty"`
	Data     string  `json:"data,omitempty"`     // base64, images only
	MIMEType string  `json:"mimeType,omitempty"` // images only
}

// The content types this transport emits, and the one image format it emits
// them in.
const (
	contentText  = "text"
	contentImage = "image"
	pngMIMEType  = "image/png"
)

// contentResult puts a tool's answer on the wire: the prose first, then one
// block per picture.
func contentResult(res Result, isErr bool) callToolResult {
	out := callToolResult{Content: []content{{Type: contentText, Text: &res.Text}}, IsError: isErr}
	for _, img := range res.Images {
		out.Content = append(out.Content, content{
			Type:     contentImage,
			Data:     base64.StdEncoding.EncodeToString(img.PNG),
			MIMEType: pngMIMEType,
		})
	}
	return out
}

// callToolParams is what tools/call sends.
type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// emptyResult is the body of a ping answer. A JSON-RPC response must carry
// either a result or an error, so the empty result is a struct and not a map:
// an empty map would be dropped by omitempty and leave a response with neither.
type emptyResult struct{}

// unknownMethodData names what the server does speak, so a client that guessed
// wrong can correct itself without reading the specification.
type unknownMethodData struct {
	Method    string   `json:"method"`
	Supported []string `json:"supportedMethods"`
}

// unknownToolData is the same courtesy for tools/call.
type unknownToolData struct {
	Tool  string   `json:"tool,omitempty"`
	Known []string `json:"knownTools"`
}
