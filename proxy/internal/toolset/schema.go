package toolset

import "encoding/json"

// The JSON Schemas are contract, not documentation: they are what a client
// validates arguments against before a call is made, and the only description
// of the tools the model ever sees besides the tool description itself. Every
// one of them is closed (additionalProperties false), so a misspelled argument
// fails in the client instead of being silently dropped here.
//
// Tool names are stable. Renaming one breaks every saved connector.
const (
	ToolGetConventions    = "get_conventions"
	ToolListFiles         = "list_files"
	ToolReadFile          = "read_file"
	ToolSearch            = "search"
	ToolWriteFile         = "write_file"
	ToolEditFile          = "edit_file"
	ToolBuild             = "build"
	ToolRender            = "render"
	ToolScreenshot        = "screenshot"
	ToolSubmit            = "submit"
	ToolListMyChanges     = "list_my_changes"
	ToolGetFeedback       = "get_feedback"
	ToolAbandonChange     = "abandon_change"
	ToolTranslationStatus = "translation_status"
	ToolBeginImageUpload  = "begin_image_upload"
)

// Defaults and caps that the schema states and the implementation enforces.
const (
	DefaultSearchResults = 100
	MaxSearchResults     = 500
	MaxTitleLen          = 120
	MaxDescriptionLen    = 4000

	// MaxHTMLBytes bounds one render. A built page of this site is around 40 kB,
	// so a whole one fits; what this stops is a page that grew unnoticed filling
	// the model's context with markup it did not ask for.
	MaxHTMLBytes = 120 << 10
)

var schemaGetConventions = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`)

var schemaListFiles = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "glob": {
      "type": "string",
      "description": "Optional filter over the whole repository-relative path, e.g. \"site/content/fi/**\" or \"site/assets/css/*.css\". Omit it to list every editable file; the tree is small enough to read whole.",
      "maxLength": 200
    }
  },
  "additionalProperties": false
}`)

var schemaReadFile = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Repository-relative path, e.g. \"site/content/fi/tapahtumat/_index.md\".",
      "minLength": 1,
      "maxLength": 512
    },
    "start": {
      "type": "integer",
      "description": "First line to return, 1-based and inclusive. Omit for the start of the file.",
      "minimum": 1
    },
    "end": {
      "type": "integer",
      "description": "Last line to return, 1-based and inclusive. Omit for the end of the file. Read a range rather than the whole of a large stylesheet.",
      "minimum": 1
    }
  },
  "required": ["path"],
  "additionalProperties": false
}`)

var schemaSearch = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "pattern": {
      "type": "string",
      "description": "Go (RE2) regular expression, matched line by line. Case-sensitive unless you write (?i).",
      "minLength": 1,
      "maxLength": 1000
    },
    "glob": {
      "type": "string",
      "description": "Optional path filter, the same form list_files takes.",
      "maxLength": 200
    },
    "max_results": {
      "type": "integer",
      "description": "Most matching lines to return. Defaults to 100.",
      "minimum": 1,
      "maximum": 500
    }
  },
  "required": ["pattern"],
  "additionalProperties": false
}`)

var schemaWriteFile = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Repository-relative path to write. Parent directories are created inside the editable tree.",
      "minLength": 1,
      "maxLength": 512
    },
    "content": {
      "type": "string",
      "description": "The whole new contents of the file. Text only, at most 2 MB; images are uploaded through Decap, not through this tool."
    }
  },
  "required": ["path", "content"],
  "additionalProperties": false
}`)

var schemaEditFile = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Repository-relative path to edit.",
      "minLength": 1,
      "maxLength": 512
    },
    "old": {
      "type": "string",
      "description": "Exact text to replace, whitespace included. It must appear exactly once in the file; include surrounding lines to make it unique.",
      "minLength": 1
    },
    "new": {
      "type": "string",
      "description": "Text to put in its place. An empty string deletes the matched text."
    }
  },
  "required": ["path", "old", "new"],
  "additionalProperties": false
}`)

var schemaBuild = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`)

var schemaRender = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "The page: either the file you edited, \"site/content/fi/tapahtumat.md\", or the address the site serves it at, \"/fi/tapahtumat/\".",
      "minLength": 1,
      "maxLength": 512
    },
    "selector": {
      "type": "string",
      "description": "Optional CSS selector, e.g. \".site-header\" or \"main .index-card\". Every match is returned. Omit it for the whole page, which is around 40 kB.",
      "maxLength": 300
    }
  },
  "required": ["path"],
  "additionalProperties": false
}`)

var schemaScreenshot = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "The page: either the file you edited, \"site/content/fi/tapahtumat.md\", or the address the site serves it at, \"/fi/tapahtumat/\".",
      "minLength": 1,
      "maxLength": 512
    },
    "width": {
      "type": "integer",
      "description": "Viewport width in pixels: 1280 for the desktop layout, 390 for a phone. Defaults to 1280.",
      "enum": [390, 1280]
    }
  },
  "required": ["path"],
  "additionalProperties": false
}`)

var schemaSubmit = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "title": {
      "type": "string",
      "description": "Pull request title, in the language of the change. It is what a maintainer reads first.",
      "minLength": 1,
      "maxLength": 120
    },
    "description": {
      "type": "string",
      "description": "The body: what changed and why. Required for a change touching site/layouts/, where it has to say what looks different now. The editor's name and the touched files are added automatically.",
      "maxLength": 4000
    }
  },
  "required": ["title"],
  "additionalProperties": false
}`)

var schemaListMyChanges = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`)

var schemaGetFeedback = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "slug": {
      "type": "string",
      "description": "Which change, as list_my_changes names them. Omit it for the change you are working on.",
      "maxLength": 64
    }
  },
  "additionalProperties": false
}`)

var schemaAbandonChange = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "slug": {
      "type": "string",
      "description": "Which change to abandon, as list_my_changes names them. Required: this closes its pull request, deletes its branch and discards its edits, so it has to be named, never guessed.",
      "minLength": 1,
      "maxLength": 64
    }
  },
  "required": ["slug"],
  "additionalProperties": false
}`)

var schemaTranslationStatus = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Optional repository-relative prefix, e.g. \"site/content/fi/kilta\". Omit it for the whole content tree.",
      "maxLength": 512
    }
  },
  "additionalProperties": false
}`)

var schemaBeginImageUpload = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "purpose": {
      "type": "string",
      "description": "What the image is for, e.g. \"vujut 2026 kuvat\". It names the change the upload lands in.",
      "maxLength": 120
    }
  },
  "additionalProperties": false
}`)

// The argument types, one per schema. They are the Go side of the same
// contract and must be changed together with it.

type listFilesArgs struct {
	Glob string `json:"glob"`
}

type readFileArgs struct {
	Path  string `json:"path"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type searchArgs struct {
	Pattern    string `json:"pattern"`
	Glob       string `json:"glob"`
	MaxResults int    `json:"max_results"`
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Old and New are pointers so that an absent property is distinguishable from
// an empty string: the schema requires both, "" is a legal value of new, and
// the two mistakes call for different advice.
type editFileArgs struct {
	Path string  `json:"path"`
	Old  *string `json:"old"`
	New  *string `json:"new"`
}

type renderArgs struct {
	Path     string `json:"path"`
	Selector string `json:"selector"`
}

type screenshotArgs struct {
	Path  string `json:"path"`
	Width int    `json:"width"`
}

type submitArgs struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type getFeedbackArgs struct {
	Slug string `json:"slug"`
}

type abandonChangeArgs struct {
	Slug string `json:"slug"`
}

type translationStatusArgs struct {
	Path string `json:"path"`
}

type beginImageUploadArgs struct {
	Purpose string `json:"purpose"`
}
