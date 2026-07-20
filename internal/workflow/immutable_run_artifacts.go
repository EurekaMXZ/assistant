package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

const (
	immutableRunRequestArtifact       = "request.json.zst"
	immutableRunResponseArtifact      = "response.json.zst"
	immutableRunOutputItemsArtifact   = "output-items.json.zst"
	immutableRunToolResultsArtifact   = "tool-results.json.zst"
	immutableRunPresentationArtifact  = "presentation-events.json.zst"
	immutableRunCheckpointArtifact    = "context-checkpoint.json.zst"
	immutableRunFailureArtifact       = "failure.json.zst"
	immutableRunArtifactSchemaVersion = 1
	immutableRunArtifactContentType   = "application/zstd"
)

func compressImmutableRunPayload(payload []byte) ([]byte, string, error) {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, "", fmt.Errorf("create zstd encoder: %w", err)
	}
	compressed := encoder.EncodeAll(payload, nil)
	encoder.Close()
	digest := sha256.Sum256(payload)
	return compressed, hex.EncodeToString(digest[:]), nil
}
