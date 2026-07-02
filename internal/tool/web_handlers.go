package tool

import (
	"context"
	"encoding/json"
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
		ChunksPerSource          int      `json:"chunks_per_source"`
		MaxResults               int      `json:"max_results"`
		TimeRange                string   `json:"time_range"`
		StartDate                string   `json:"start_date"`
		EndDate                  string   `json:"end_date"`
		Days                     int      `json:"days"`
		IncludeAnswer            any      `json:"include_answer"`
		IncludeRawContent        any      `json:"include_raw_content"`
		IncludeImages            bool     `json:"include_images"`
		IncludeImageDescriptions bool     `json:"include_image_descriptions"`
		IncludeFavicon           bool     `json:"include_favicon"`
		IncludeUsage             bool     `json:"include_usage"`
		IncludeDomains           []string `json:"include_domains"`
		ExcludeDomains           []string `json:"exclude_domains"`
		Country                  string   `json:"country"`
		AutoParameters           bool     `json:"auto_parameters"`
		ExactMatch               bool     `json:"exact_match"`
	}
	if err := decodeToolArguments(call, WebSearch, &input); err != nil {
		return nil, err
	}

	result, err := h.UseCase.Search(ctx, SearchWebInput{
		Query:                    input.Query,
		Topic:                    input.Topic,
		SearchDepth:              input.SearchDepth,
		ChunksPerSource:          input.ChunksPerSource,
		MaxResults:               input.MaxResults,
		TimeRange:                input.TimeRange,
		StartDate:                input.StartDate,
		EndDate:                  input.EndDate,
		Days:                     input.Days,
		IncludeAnswer:            input.IncludeAnswer,
		IncludeRawContent:        input.IncludeRawContent,
		IncludeImages:            input.IncludeImages,
		IncludeImageDescriptions: input.IncludeImageDescriptions,
		IncludeFavicon:           input.IncludeFavicon,
		IncludeUsage:             input.IncludeUsage,
		IncludeDomains:           input.IncludeDomains,
		ExcludeDomains:           input.ExcludeDomains,
		Country:                  input.Country,
		AutoParameters:           input.AutoParameters,
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
		URLs             []string        `json:"urls"`
		ExtractDepth     string          `json:"extract_depth"`
		Format           string          `json:"format"`
		Timeout          float64         `json:"timeout"`
		IncludeImages    bool            `json:"include_images"`
		IncludeFavicon   bool            `json:"include_favicon"`
		IncludeUsage     bool            `json:"include_usage"`
		Query            string          `json:"query"`
		ChunksPerSource  int             `json:"chunks_per_source"`
		ExtractionPrompt string          `json:"extraction_prompt"`
		Schema           json.RawMessage `json:"schema"`
	}
	if err := decodeToolArguments(call, WebExtract, &input); err != nil {
		return nil, err
	}

	payload, err := h.UseCase.Extract(ctx, ExtractWebInput{
		URLs:             input.URLs,
		ExtractDepth:     input.ExtractDepth,
		Format:           input.Format,
		Timeout:          input.Timeout,
		IncludeImages:    input.IncludeImages,
		IncludeFavicon:   input.IncludeFavicon,
		IncludeUsage:     input.IncludeUsage,
		Query:            input.Query,
		ChunksPerSource:  input.ChunksPerSource,
		ExtractionPrompt: input.ExtractionPrompt,
		Schema:           input.Schema,
	})
	if err != nil {
		return nil, err
	}
	return outputOnlyExecutionResult(call.CallID, payload), nil
}
