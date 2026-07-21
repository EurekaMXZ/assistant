package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

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

func immutableRunArtifactChecksum(raw json.RawMessage, name string) string {
	if len(raw) == 0 || name == "" {
		return ""
	}
	var metadata map[string]RunArtifactMetadata
	if json.Unmarshal(raw, &metadata) != nil {
		return ""
	}
	return metadata[name].SHA256
}

func immutableRunPayloadMatchesChecksum(payload []byte, expected string) bool {
	if strings.TrimSpace(expected) == "" {
		return true
	}
	digest := sha256.Sum256(payload)
	return strings.EqualFold(expected, hex.EncodeToString(digest[:]))
}

func decompressImmutableRunPayload(compressed []byte) ([]byte, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("create immutable artifact decoder: %w", err)
	}
	payload, err := decoder.DecodeAll(compressed, nil)
	decoder.Close()
	if err != nil {
		return nil, fmt.Errorf("decompress immutable artifact: %w", err)
	}
	return payload, nil
}
