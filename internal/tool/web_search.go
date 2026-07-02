package tool

import (
	"context"
	"encoding/json"

	"github.com/EurekaMXZ/assistant/internal/tavily"
)

type TavilyGateway interface {
	Search(ctx context.Context, request tavily.SearchRequest) (*tavily.SearchResponse, error)
	Extract(ctx context.Context, request tavily.ExtractRequest) (json.RawMessage, error)
}
