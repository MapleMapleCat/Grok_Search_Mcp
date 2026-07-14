package grok

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// citationCollector 对 URL 去重，同时维护扁平 citations 列表与带标题的 sources。
type citationCollector struct {
	citations       []string
	sources         []Source
	seen            map[string]struct{}
	aggregatedBytes int
	err             error
}

func newCitationCollector() *citationCollector {
	return &citationCollector{seen: make(map[string]struct{})}
}

func (c *citationCollector) add(url, title string) {
	if c.err != nil {
		return
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return
	}
	if _, ok := c.seen[url]; ok {
		return
	}
	if len(c.citations) >= maxCitationCount {
		c.err = fmt.Errorf("upstream response exceeded citation limit of %d", maxCitationCount)
		return
	}
	title = strings.TrimSpace(title)
	additionalBytes := len(url) + len(title)
	if additionalBytes > maxAggregatedCitationBytes || c.aggregatedBytes > maxAggregatedCitationBytes-additionalBytes {
		c.err = fmt.Errorf("upstream response exceeded aggregated citation byte limit of %d", maxAggregatedCitationBytes)
		return
	}
	c.seen[url] = struct{}{}
	c.aggregatedBytes += additionalBytes
	c.citations = append(c.citations, url)
	c.sources = append(c.sources, Source{
		URL:   url,
		Title: title,
	})
}

// addRaw normalizes common OpenAI/CPA citation aliases and nested extension
// shapes while requiring an explicit URL before exposing a source.
func (c *citationCollector) addRaw(raw json.RawMessage) {
	if c.err != nil {
		return
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err == nil {
			for _, item := range items {
				c.addRaw(item)
			}
		}
		return
	}
	if raw[0] == '"' {
		var url string
		if err := json.Unmarshal(raw, &url); err == nil {
			c.add(url, "")
		}
		return
	}

	type citationExtension struct {
		URL         string          `json:"url"`
		URI         string          `json:"uri"`
		SourceURL   string          `json:"source_url"`
		Title       string          `json:"title"`
		Name        string          `json:"name"`
		URLCitation json.RawMessage `json:"url_citation"`
		Citation    json.RawMessage `json:"citation"`
		Source      json.RawMessage `json:"source"`
	}

	var citation citationExtension
	if err := json.Unmarshal(raw, &citation); err != nil {
		return
	}
	url := firstNonEmptyString(citation.URL, citation.URI, citation.SourceURL)
	title := firstNonEmptyString(citation.Title, citation.Name)
	if url != "" {
		c.add(url, title)
	}
	for _, nestedCitation := range []json.RawMessage{citation.URLCitation, citation.Citation, citation.Source} {
		if len(bytes.TrimSpace(nestedCitation)) > 0 && string(bytes.TrimSpace(nestedCitation)) != "null" {
			c.addRaw(nestedCitation)
		}
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if normalizedValue := strings.TrimSpace(value); normalizedValue != "" {
			return normalizedValue
		}
	}
	return ""
}

// buildSearchResult 从 output 文本块、注解与顶层 citations 汇总答案与引用。
func buildSearchResult(parsed responsesResponse, rawBody []byte) (*SearchResult, error) {
	var answer strings.Builder
	collector := newCitationCollector()

	for _, item := range parsed.Output {
		for _, block := range item.Content {
			if text := strings.TrimSpace(block.Text); text != "" {
				if answer.Len() > 0 {
					if err := appendAnswerText(&answer, "\n", text); err != nil {
						return nil, err
					}
				} else if err := appendAnswerText(&answer, text); err != nil {
					return nil, err
				}
			}
			for _, ann := range block.Annotations {
				if ann.Type == "url_citation" {
					collector.add(ann.URL, ann.Title)
				}
			}
		}
	}

	if len(parsed.Citations) > 0 && string(parsed.Citations) != "null" {
		var rawCites []json.RawMessage
		if err := json.Unmarshal(parsed.Citations, &rawCites); err == nil {
			for _, rc := range rawCites {
				collector.addRaw(rc)
			}
		}
	}
	if collector.err != nil {
		return nil, collector.err
	}

	usage, err := parseUsage(parsed.Usage)
	if err != nil {
		return nil, err
	}

	answerText := strings.TrimSpace(answer.String())
	if answerText == "" {
		return nil, fmt.Errorf("upstream response did not contain answer text")
	}

	return &SearchResult{
		Answer:      answerText,
		Citations:   collector.citations,
		Sources:     collector.sources,
		Usage:       usage,
		RawResponse: json.RawMessage(rawBody),
	}, nil
}

func parseUsage(raw json.RawMessage) (*Usage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var usage Usage
	if err := json.Unmarshal(raw, &usage); err != nil {
		return nil, fmt.Errorf("decode usage: %w", err)
	}
	return &usage, nil
}
