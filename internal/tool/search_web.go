package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/tavily"
)

type SearchWebInput struct {
	Query                    string
	Topic                    string
	SearchDepth              string
	ChunksPerSource          int
	MaxResults               int
	TimeRange                string
	StartDate                string
	EndDate                  string
	Days                     int
	IncludeAnswer            any
	IncludeRawContent        any
	IncludeImages            bool
	IncludeImageDescriptions bool
	IncludeFavicon           bool
	IncludeUsage             bool
	IncludeDomains           []string
	ExcludeDomains           []string
	Country                  string
	AutoParameters           bool
	ExactMatch               bool
}

type ExtractWebInput struct {
	URLs             []string
	ExtractDepth     string
	Format           string
	Timeout          float64
	IncludeImages    bool
	IncludeFavicon   bool
	IncludeUsage     bool
	Query            string
	ChunksPerSource  int
	ExtractionPrompt string
	Schema           json.RawMessage
}

type TavilyTools struct {
	Client TavilyGateway
}

func (uc TavilyTools) Search(ctx context.Context, input SearchWebInput) (*tavily.SearchResponse, error) {
	if uc.Client == nil {
		return nil, errors.New("tavily tools require tavily client")
	}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		return nil, errors.New("query is required")
	}

	topic, err := normalizeOptionalEnum("topic", strings.ToLower(strings.TrimSpace(input.Topic)), "general", "general", "news")
	if err != nil {
		return nil, err
	}
	searchDepth, err := normalizeOptionalEnum("search_depth", strings.ToLower(strings.TrimSpace(input.SearchDepth)), "basic", "basic", "advanced", "fast", "ultra-fast")
	if err != nil {
		return nil, err
	}

	timeRange := strings.ToLower(strings.TrimSpace(input.TimeRange))
	if _, err := normalizeOptionalEnum("time_range", timeRange, "", "day", "week", "month", "year", "d", "w", "m", "y"); err != nil {
		return nil, err
	}

	maxResults := input.MaxResults
	if maxResults < 0 {
		return nil, errors.New("max_results cannot be negative")
	}
	if maxResults == 0 {
		maxResults = 5
	}
	if maxResults > 20 {
		maxResults = 20
	}
	if input.ChunksPerSource < 0 {
		return nil, errors.New("chunks_per_source cannot be negative")
	}
	if input.ChunksPerSource > 3 {
		return nil, errors.New("chunks_per_source cannot be greater than 3")
	}
	if input.Days < 0 {
		return nil, errors.New("days cannot be negative")
	}

	includeAnswer, err := normalizeBoolStringOption("include_answer", input.IncludeAnswer, "basic", "advanced")
	if err != nil {
		return nil, err
	}
	includeRawContent, err := normalizeBoolStringOption("include_raw_content", input.IncludeRawContent, "markdown", "text")
	if err != nil {
		return nil, err
	}

	return uc.Client.Search(ctx, tavily.SearchRequest{
		Query:                    query,
		Topic:                    topic,
		SearchDepth:              searchDepth,
		ChunksPerSource:          input.ChunksPerSource,
		MaxResults:               maxResults,
		TimeRange:                timeRange,
		StartDate:                strings.TrimSpace(input.StartDate),
		EndDate:                  strings.TrimSpace(input.EndDate),
		Days:                     input.Days,
		IncludeAnswer:            includeAnswer,
		IncludeRawContent:        includeRawContent,
		IncludeImages:            input.IncludeImages,
		IncludeImageDescriptions: input.IncludeImageDescriptions,
		IncludeFavicon:           input.IncludeFavicon,
		IncludeUsage:             input.IncludeUsage,
		IncludeDomains:           compactSearchValues(input.IncludeDomains),
		ExcludeDomains:           compactSearchValues(input.ExcludeDomains),
		Country:                  strings.ToLower(strings.TrimSpace(input.Country)),
		AutoParameters:           input.AutoParameters,
		ExactMatch:               input.ExactMatch,
	})
}

func (uc TavilyTools) Extract(ctx context.Context, input ExtractWebInput) (json.RawMessage, error) {
	if uc.Client == nil {
		return nil, errors.New("tavily tools require tavily client")
	}
	urls := compactSearchValues(input.URLs)
	if len(urls) == 0 {
		return nil, errors.New("urls is required")
	}
	extractDepth, err := normalizeOptionalEnum("extract_depth", strings.ToLower(strings.TrimSpace(input.ExtractDepth)), "", "basic", "advanced")
	if err != nil {
		return nil, err
	}
	format, err := normalizeOptionalEnum("format", strings.ToLower(strings.TrimSpace(input.Format)), "", "markdown", "text")
	if err != nil {
		return nil, err
	}
	if input.Timeout < 0 {
		return nil, errors.New("timeout cannot be negative")
	}
	if input.ChunksPerSource < 0 {
		return nil, errors.New("chunks_per_source cannot be negative")
	}
	if input.ChunksPerSource > 5 {
		return nil, errors.New("chunks_per_source cannot be greater than 5")
	}

	return uc.Client.Extract(ctx, tavily.ExtractRequest{
		URLs:             urls,
		ExtractDepth:     extractDepth,
		Format:           format,
		Timeout:          input.Timeout,
		IncludeImages:    input.IncludeImages,
		IncludeFavicon:   input.IncludeFavicon,
		IncludeUsage:     input.IncludeUsage,
		Query:            strings.TrimSpace(input.Query),
		ChunksPerSource:  input.ChunksPerSource,
		ExtractionPrompt: strings.TrimSpace(input.ExtractionPrompt),
		Schema:           compactRawJSON(input.Schema),
	})
}

func compactSearchValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	compacted := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		compacted = append(compacted, value)
	}
	if len(compacted) == 0 {
		return nil
	}
	return compacted
}

func compactRawJSON(value json.RawMessage) json.RawMessage {
	value = json.RawMessage(strings.TrimSpace(string(value)))
	if len(value) == 0 || string(value) == "null" {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func normalizeOptionalEnum(name string, value string, defaultValue string, allowed ...string) (string, error) {
	if value == "" {
		return defaultValue, nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "", errors.New(name + " is not supported")
}

func normalizeBoolStringOption(name string, value any, allowedStrings ...string) (any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case bool:
		return typed, nil
	case string:
		typed = strings.ToLower(strings.TrimSpace(typed))
		if typed == "" {
			return nil, nil
		}
		for _, allowed := range allowedStrings {
			if typed == allowed {
				return typed, nil
			}
		}
		return nil, errors.New(name + " string value is not supported")
	default:
		return nil, errors.New(name + " must be a boolean or string")
	}
}
