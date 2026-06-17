package grok

import "encoding/json"

type responsesRequest struct {
	Model  string         `json:"model"`
	Input  []inputMessage `json:"input"`
	Tools  []toolDef      `json:"tools"`
	Stream bool           `json:"stream"`
}

type inputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// toolDef describes a single upstream built-in tool.
type toolDef struct {
	Type                     string   `json:"type"`
	AllowedDomains           []string `json:"allowed_domains,omitempty"`
	ExcludedDomains          []string `json:"excluded_domains,omitempty"`
	EnableImageUnderstanding *bool    `json:"enable_image_understanding,omitempty"`
	EnableImageSearch        *bool    `json:"enable_image_search,omitempty"`
}

type responsesResponse struct {
	Output    []outputItem    `json:"output"`
	Usage     json.RawMessage `json:"usage"`
	Citations json.RawMessage `json:"citations"`
}

type outputItem struct {
	Type    string         `json:"type"`
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type        string       `json:"type"`
	Text        string       `json:"text"`
	Annotations []annotation `json:"annotations"`
}

type annotation struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

type citationItem struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type streamEvent struct {
	Type     string            `json:"type"`
	Item     streamOutputItem  `json:"item"`
	Response responsesResponse `json:"response"`
}

type streamOutputItem struct {
	Type   string          `json:"type"`
	Action webSearchAction `json:"action"`
}

type webSearchAction struct {
	Query string `json:"query"`
	URL   string `json:"url"`
}