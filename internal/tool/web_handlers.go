package tool

import (
	"context"
)

type SearchWebHandler struct {
	UseCase TavilyTools
}

func (h SearchWebHandler) ToolName() string {
	return WebSearch
}

func (h SearchWebHandler) Execute(ctx context.Context, _ ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var input struct {
		Query                    string   `json:"query"`
		Topic                    string   `json:"topic"`
		SearchDepth              string   `json:"search_depth"`
		MaxResults               float64  `json:"max_results"`
		TimeRange                string   `json:"time_range"`
		StartDate                string   `json:"start_date"`
		EndDate                  string   `json:"end_date"`
		IncludeImages            bool     `json:"include_images"`
		IncludeImageDescriptions bool     `json:"include_image_descriptions"`
		IncludeFavicon           bool     `json:"include_favicon"`
		IncludeDomains           []string `json:"include_domains"`
		ExcludeDomains           []string `json:"exclude_domains"`
		Country                  string   `json:"country"`
		ExactMatch               bool     `json:"exact_match"`
	}
	if err := decodeToolArguments(call, WebSearch, &input); err != nil {
		return nil, err
	}

	result, err := h.UseCase.Search(ctx, SearchWebInput{
		Query:                    input.Query,
		Topic:                    input.Topic,
		SearchDepth:              input.SearchDepth,
		MaxResults:               input.MaxResults,
		TimeRange:                input.TimeRange,
		StartDate:                input.StartDate,
		EndDate:                  input.EndDate,
		IncludeImages:            input.IncludeImages,
		IncludeImageDescriptions: input.IncludeImageDescriptions,
		IncludeFavicon:           input.IncludeFavicon,
		IncludeDomains:           input.IncludeDomains,
		ExcludeDomains:           input.ExcludeDomains,
		Country:                  input.Country,
		ExactMatch:               input.ExactMatch,
	})
	if err != nil {
		return nil, err
	}

	payload, err := marshalToolOutput(WebSearch, result)
	if err != nil {
		return nil, err
	}

	return outputOnlyExecutionResult(call.CallID, payload), nil
}

type ExtractWebHandler struct {
	UseCase TavilyTools
}

func (h ExtractWebHandler) ToolName() string {
	return WebExtract
}

func (h ExtractWebHandler) Execute(ctx context.Context, _ ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var input struct {
		URLs           []string `json:"urls"`
		ExtractDepth   string   `json:"extract_depth"`
		Format         string   `json:"format"`
		IncludeImages  bool     `json:"include_images"`
		IncludeFavicon bool     `json:"include_favicon"`
		Query          string   `json:"query"`
	}
	if err := decodeToolArguments(call, WebExtract, &input); err != nil {
		return nil, err
	}

	payload, err := h.UseCase.Extract(ctx, ExtractWebInput{
		URLs:           input.URLs,
		ExtractDepth:   input.ExtractDepth,
		Format:         input.Format,
		IncludeImages:  input.IncludeImages,
		IncludeFavicon: input.IncludeFavicon,
		Query:          input.Query,
	})
	if err != nil {
		return nil, err
	}
	return outputOnlyExecutionResult(call.CallID, payload), nil
}
