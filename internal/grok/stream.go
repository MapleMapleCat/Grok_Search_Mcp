package grok

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/grok-mcp/internal/logx"
)

type searchRoundTracker struct {
	nextRound int
	seen      map[string]struct{}
}

type compatibleSearchEvent struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Query     string          `json:"query"`
	URL       string          `json:"url"`
	Action    webSearchAction `json:"action"`
	Search    webSearchAction `json:"search"`
	WebSearch webSearchAction `json:"web_search"`
	XSearch   webSearchAction `json:"x_search"`
	Item      struct {
		ID     string          `json:"id"`
		Type   string          `json:"type"`
		Action webSearchAction `json:"action"`
		Query  string          `json:"query"`
		URL    string          `json:"url"`
	} `json:"item"`
}

func newSearchRoundTracker() *searchRoundTracker {
	return &searchRoundTracker{seen: make(map[string]struct{})}
}

// emitCompatibleSearchRound recognizes completed Responses events and a small
// allow-list of CPA extension event names. Unknown events are ignored so normal
// Chat Completions chunks cannot create false progress notifications.
func (t *searchRoundTracker) emitCompatibleSearchRound(payload string, onRound func(SearchRound), log *logx.Logger) error {
	var event compatibleSearchEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return fmt.Errorf("decode search event: %w", err)
	}

	itemType, completed := compatibleSearchEventType(event)
	if !completed {
		return nil
	}

	query := firstNonEmptyString(
		event.Item.Action.Query,
		event.Item.Query,
		event.Action.Query,
		event.Search.Query,
		event.WebSearch.Query,
		event.XSearch.Query,
		event.Query,
	)
	url := firstNonEmptyString(
		event.Item.Action.URL,
		event.Item.URL,
		event.Action.URL,
		event.Search.URL,
		event.WebSearch.URL,
		event.XSearch.URL,
		event.URL,
	)
	eventID := firstNonEmptyString(event.Item.ID, event.ID)
	deduplicationKey := eventID
	if deduplicationKey == "" {
		deduplicationKey = itemType + "\x00" + query + "\x00" + url
	}
	if _, alreadyEmitted := t.seen[deduplicationKey]; alreadyEmitted {
		return nil
	}
	if t.nextRound >= maxSearchRoundCount {
		return fmt.Errorf("upstream stream exceeded search round limit of %d", maxSearchRoundCount)
	}
	t.seen[deduplicationKey] = struct{}{}

	t.nextRound++
	searchRound := SearchRound{Round: t.nextRound, Query: query, URL: url}
	logStreamRound(log, itemType, searchRound)
	if onRound != nil {
		onRound(searchRound)
	}
	return nil
}

func compatibleSearchEventType(event compatibleSearchEvent) (string, bool) {
	if event.Type == "response.output_item.done" && isSearchCallItem(event.Item.Type) {
		return event.Item.Type, true
	}

	switch event.Type {
	case "web_search_call.done", "web_search_call.completed",
		"web_search.done", "web_search.completed",
		"response.web_search_call.done", "response.web_search_call.completed",
		"response.web_search.done", "response.web_search.completed":
		return "web_search_call", true
	case "x_search_call.done", "x_search_call.completed",
		"x_search.done", "x_search.completed",
		"response.x_search_call.done", "response.x_search_call.completed",
		"response.x_search.done", "response.x_search.completed":
		return "x_search_call", true
	case "search.done", "search.completed":
		switch event.Item.Type {
		case "web_search", "web_search_call":
			return "web_search_call", true
		case "x_search", "x_search_call":
			return "x_search_call", true
		}
		if firstNonEmptyString(event.WebSearch.Query, event.WebSearch.URL) != "" {
			return "web_search_call", true
		}
		if firstNonEmptyString(event.XSearch.Query, event.XSearch.URL) != "" {
			return "x_search_call", true
		}
	}
	return "", false
}

// parseSearchStream 消费上游 SSE，在 web_search_call 或 x_search_call 完成时回调 onRound，
// 并在收到 response.completed 后从该事件的 response 字段构建 SearchResult。
func parseSearchStream(body io.Reader, onRound func(SearchRound), log *logx.Logger) (*SearchResult, error) {
	var completedBody []byte
	searchRounds := newSearchRoundTracker()

	err := forEachSSEEvent(body, func(payload string) error {
		var event streamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return fmt.Errorf("decode stream event: %w", err)
		}

		switch event.Type {
		case "response.output_item.done":
			// action（query/url）只在 output_item.done 时才完整。
			// CPA 源码证据：output_item.added 的 item 只有 {id,type,status}，无 action。
			if err := searchRounds.emitCompatibleSearchRound(payload, onRound, log); err != nil {
				return err
			}
		case "response.completed":
			completedBody = []byte(payload)
		case "error":
			return fmt.Errorf("upstream stream error: %s", logx.Truncate(payload, 1024))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(completedBody) == 0 {
		return nil, fmt.Errorf("upstream stream ended without response.completed event")
	}

	var completed streamEvent
	if err := json.Unmarshal(completedBody, &completed); err != nil {
		return nil, fmt.Errorf("decode response.completed: %w", err)
	}

	return buildSearchResult(completed.Response, completedBody)
}

func isSearchCallItem(itemType string) bool {
	return itemType == "web_search_call" || itemType == "x_search_call"
}

func logStreamRound(log *logx.Logger, itemType string, searchRound SearchRound) {
	if log == nil {
		return
	}
	if searchRound.Query != "" {
		log.Debugf("%s round=%d query=%q", itemType, searchRound.Round, searchRound.Query)
	} else if searchRound.URL != "" {
		log.Debugf("%s round=%d url=%s", itemType, searchRound.Round, searchRound.URL)
	} else {
		log.Debugf("%s round=%d", itemType, searchRound.Round)
	}
}
