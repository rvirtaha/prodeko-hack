package mcpserver

import (
	"encoding/json"
	"strconv"
)

// The prompts and resources side of the transport. Both are static documents
// declared at construction: a prompt is a pre-written way in ("post an
// announcement"), a resource is a document a client fetches by URI — the
// upload widget's HTML under the MCP Apps extension lives here. Nothing is
// listed that was not configured, and nothing here runs code.

// Prompt is one pre-written entry point. Text is the whole content: the
// prompts this server carries are instructions, not templates, so there are
// no arguments to fill in.
type Prompt struct {
	Name        string
	Description string
	Text        string
}

// Resource is one document, served whole. Meta travels as the content's
// _meta, which is where the MCP Apps extension reads its ui block (csp,
// permissions) from.
type Resource struct {
	URI         string
	Name        string
	Description string
	MimeType    string
	Text        string
	Meta        json.RawMessage
}

type promptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type resourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type promptDescriptor struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type promptsListResult struct {
	Prompts []promptDescriptor `json:"prompts"`
}

type promptsGetParams struct {
	Name string `json:"name"`
}

type promptMessage struct {
	Role    string      `json:"role"`
	Content textContent `json:"content"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type promptsGetResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []promptMessage `json:"messages"`
}

type resourceDescriptor struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type resourcesListResult struct {
	Resources []resourceDescriptor `json:"resources"`
}

type resourcesReadParams struct {
	URI string `json:"uri"`
}

type resourceContents struct {
	URI      string          `json:"uri"`
	MimeType string          `json:"mimeType,omitempty"`
	Text     string          `json:"text"`
	Meta     json.RawMessage `json:"_meta,omitempty"`
}

type resourcesReadResult struct {
	Contents []resourceContents `json:"contents"`
}

func (s *Server) listPrompts(req request) *response {
	out := make([]promptDescriptor, 0, len(s.cfg.Prompts))
	for _, p := range s.cfg.Prompts {
		out = append(out, promptDescriptor{Name: p.Name, Description: p.Description})
	}
	return resultResponse(req.ID, promptsListResult{Prompts: out})
}

func (s *Server) getPrompt(req request) *response {
	var p promptsGetParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, codeInvalidParams, "the prompts/get params could not be read", err.Error())
	}
	for _, prompt := range s.cfg.Prompts {
		if prompt.Name == p.Name {
			return resultResponse(req.ID, promptsGetResult{
				Description: prompt.Description,
				Messages: []promptMessage{{
					Role:    "user",
					Content: textContent{Type: "text", Text: prompt.Text},
				}},
			})
		}
	}
	return errorResponse(req.ID, codeInvalidParams, "unknown prompt "+strconv.Quote(p.Name), nil)
}

func (s *Server) listResources(req request) *response {
	out := make([]resourceDescriptor, 0, len(s.cfg.Resources))
	for _, r := range s.cfg.Resources {
		out = append(out, resourceDescriptor{URI: r.URI, Name: r.Name, Description: r.Description, MimeType: r.MimeType})
	}
	return resultResponse(req.ID, resourcesListResult{Resources: out})
}

func (s *Server) readResource(req request) *response {
	var p resourcesReadParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, codeInvalidParams, "the resources/read params could not be read", err.Error())
	}
	for _, r := range s.cfg.Resources {
		if r.URI == p.URI {
			return resultResponse(req.ID, resourcesReadResult{
				Contents: []resourceContents{{URI: r.URI, MimeType: r.MimeType, Text: r.Text, Meta: r.Meta}},
			})
		}
	}
	return errorResponse(req.ID, codeInvalidParams, "unknown resource "+strconv.Quote(p.URI), nil)
}
