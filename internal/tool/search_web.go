package tool

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/tavily"
)

type SearchWebInput struct {
	Query                    string
	Topic                    string
	SearchDepth              string
	MaxResults               float64
	TimeRange                string
	StartDate                string
	EndDate                  string
	IncludeImages            bool
	IncludeImageDescriptions bool
	IncludeFavicon           bool
	IncludeDomains           []string
	ExcludeDomains           []string
	Country                  string
	ExactMatch               bool
}

type ExtractWebInput struct {
	URLs           []string
	ExtractDepth   string
	Format         string
	IncludeImages  bool
	IncludeFavicon bool
	Query          string
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

	topic, err := normalizeOptionalEnum("topic", strings.ToLower(strings.TrimSpace(input.Topic)), "general", "general")
	if err != nil {
		return nil, err
	}
	searchDepth, err := normalizeOptionalEnum("search_depth", strings.ToLower(strings.TrimSpace(input.SearchDepth)), "basic", "basic", "advanced", "fast", "ultra-fast")
	if err != nil {
		return nil, err
	}

	timeRange := strings.ToLower(strings.TrimSpace(input.TimeRange))
	if _, err := normalizeOptionalEnum("time_range", timeRange, "", "day", "week", "month", "year"); err != nil {
		return nil, err
	}
	startDate := strings.TrimSpace(input.StartDate)
	endDate := strings.TrimSpace(input.EndDate)
	if startDate != "" || endDate != "" {
		timeRange = ""
	}

	maxResults := input.MaxResults
	if maxResults == 0 {
		maxResults = 5
	}
	if math.Trunc(maxResults) != maxResults {
		return nil, errors.New("max_results must be an integer")
	}
	if maxResults < 5 {
		return nil, errors.New("max_results cannot be less than 5")
	}
	if maxResults > 20 {
		return nil, errors.New("max_results cannot be greater than 20")
	}

	return uc.Client.Search(ctx, tavily.SearchRequest{
		Query:                    query,
		Topic:                    topic,
		SearchDepth:              searchDepth,
		MaxResults:               int(maxResults),
		TimeRange:                timeRange,
		StartDate:                startDate,
		EndDate:                  endDate,
		IncludeRawContent:        false,
		IncludeImages:            input.IncludeImages,
		IncludeImageDescriptions: input.IncludeImageDescriptions,
		IncludeFavicon:           input.IncludeFavicon,
		IncludeDomains:           compactSearchValues(input.IncludeDomains),
		ExcludeDomains:           compactSearchValues(input.ExcludeDomains),
		Country:                  strings.ToLower(strings.TrimSpace(input.Country)),
		ExactMatch:               input.ExactMatch && tavily.CanUseExactMatch(query),
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
	extractDepth, err := normalizeOptionalEnum("extract_depth", strings.ToLower(strings.TrimSpace(input.ExtractDepth)), "basic", "basic", "advanced")
	if err != nil {
		return nil, err
	}
	format, err := normalizeOptionalEnum("format", strings.ToLower(strings.TrimSpace(input.Format)), "markdown", "markdown", "text")
	if err != nil {
		return nil, err
	}
	return uc.Client.Extract(ctx, tavily.ExtractRequest{
		URLs:           urls,
		ExtractDepth:   extractDepth,
		Format:         format,
		IncludeImages:  input.IncludeImages,
		IncludeFavicon: input.IncludeFavicon,
		Query:          strings.TrimSpace(input.Query),
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
