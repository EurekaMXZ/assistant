package tavily

import "encoding/json"

type SearchRequest struct {
	Query                    string   `json:"query"`
	Topic                    string   `json:"topic,omitempty"`
	SearchDepth              string   `json:"search_depth,omitempty"`
	ChunksPerSource          int      `json:"chunks_per_source,omitempty"`
	MaxResults               int      `json:"max_results,omitempty"`
	TimeRange                string   `json:"time_range,omitempty"`
	StartDate                string   `json:"start_date,omitempty"`
	EndDate                  string   `json:"end_date,omitempty"`
	Days                     int      `json:"days,omitempty"`
	IncludeAnswer            any      `json:"include_answer,omitempty"`
	IncludeRawContent        any      `json:"include_raw_content,omitempty"`
	IncludeImages            bool     `json:"include_images,omitempty"`
	IncludeImageDescriptions bool     `json:"include_image_descriptions,omitempty"`
	IncludeFavicon           bool     `json:"include_favicon,omitempty"`
	IncludeUsage             bool     `json:"include_usage,omitempty"`
	IncludeDomains           []string `json:"include_domains,omitempty"`
	ExcludeDomains           []string `json:"exclude_domains,omitempty"`
	Country                  string   `json:"country,omitempty"`
	AutoParameters           bool     `json:"auto_parameters,omitempty"`
	ExactMatch               bool     `json:"exact_match,omitempty"`
}

type SearchResponse struct {
	Query        string         `json:"query"`
	Answer       string         `json:"answer,omitempty"`
	Results      []SearchResult `json:"results,omitempty"`
	Images       []SearchImage  `json:"images,omitempty"`
	ResponseTime string         `json:"response_time,omitempty"`
	RequestID    string         `json:"request_id,omitempty"`
	Usage        map[string]any `json:"usage,omitempty"`
}

type SearchResult struct {
	Title         string        `json:"title"`
	URL           string        `json:"url"`
	Content       string        `json:"content,omitempty"`
	RawContent    string        `json:"raw_content,omitempty"`
	Score         float64       `json:"score,omitempty"`
	PublishedDate string        `json:"published_date,omitempty"`
	Favicon       string        `json:"favicon,omitempty"`
	FaviconURL    string        `json:"favicon_url,omitempty"`
	Domain        string        `json:"domain,omitempty"`
	Rank          int           `json:"rank,omitempty"`
	Images        []SearchImage `json:"images,omitempty"`
}

type SearchImage struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

func (i *SearchImage) UnmarshalJSON(data []byte) error {
	var url string
	if err := json.Unmarshal(data, &url); err == nil {
		i.URL = url
		return nil
	}

	var object struct {
		URL         string `json:"url"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	i.URL = object.URL
	i.Description = object.Description
	return nil
}

type ExtractRequest struct {
	URLs             []string        `json:"urls"`
	ExtractDepth     string          `json:"extract_depth,omitempty"`
	Format           string          `json:"format,omitempty"`
	Timeout          float64         `json:"timeout,omitempty"`
	IncludeImages    bool            `json:"include_images,omitempty"`
	IncludeFavicon   bool            `json:"include_favicon,omitempty"`
	IncludeUsage     bool            `json:"include_usage,omitempty"`
	Query            string          `json:"query,omitempty"`
	ChunksPerSource  int             `json:"chunks_per_source,omitempty"`
	ExtractionPrompt string          `json:"extraction_prompt,omitempty"`
	Schema           json.RawMessage `json:"schema,omitempty"`
}
