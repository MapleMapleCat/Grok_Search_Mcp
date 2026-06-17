package grok

import "encoding/json"

type ToolType string

const (
	ToolTypeWebSearch ToolType = "web_search"
	ToolTypeXSearch   ToolType = "x_search"
)

type SearchRequest struct {
	Query                    string
	Model                    string
	ToolType                 ToolType
	AllowedDomains           []string
	ExcludedDomains          []string
	EnableImageUnderstanding *bool
	EnableImageSearch        *bool
}

type Usage struct {
	InputTokens     int `json:"input_tokens,omitempty"`
	OutputTokens    int `json:"output_tokens,omitempty"`
	TotalTokens     int `json:"total_tokens,omitempty"`
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// Source is a cited reference with optional title from url_citation.
type Source struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// SearchRound describes one upstream search/fetch action during streaming.
type SearchRound struct {
	Round int
	Query string
	URL   string
}

type SearchResult struct {
	Answer      string
	Citations   []string
	Sources     []Source
	Usage       *Usage
	RawResponse json.RawMessage
}