package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/vmihailenco/msgpack/v5"
)

const ContextSnapshotSchemaVersion = 1

func EncodeContextSnapshot(snapshot *ContextSnapshot) ([]byte, error) {
	if snapshot == nil {
		return nil, errors.New("context snapshot is required")
	}
	if containsInlineImage(snapshot) {
		return nil, errors.New("context snapshot contains inline image data")
	}
	encoded := cloneContext(snapshot)
	if encoded.SchemaVersion <= 0 {
		encoded.SchemaVersion = ContextSnapshotSchemaVersion
	}
	if encoded.CreatedAt.IsZero() {
		encoded.CreatedAt = time.Now().UTC()
	}
	encoded.Checksum = ""
	payload, err := msgpack.Marshal(encoded)
	if err != nil {
		return nil, fmt.Errorf("marshal context snapshot: %w", err)
	}
	digest := sha256.Sum256(payload)
	encoded.Checksum = hex.EncodeToString(digest[:])
	payload, err = msgpack.Marshal(encoded)
	if err != nil {
		return nil, fmt.Errorf("marshal checksummed context snapshot: %w", err)
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, fmt.Errorf("create context snapshot encoder: %w", err)
	}
	compressed := encoder.EncodeAll(payload, nil)
	encoder.Close()
	return compressed, nil
}

func DecodeContextSnapshot(compressed []byte) (*ContextSnapshot, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("create context snapshot decoder: %w", err)
	}
	payload, err := decoder.DecodeAll(compressed, nil)
	decoder.Close()
	if err != nil {
		return nil, fmt.Errorf("decompress context snapshot: %w", err)
	}
	var snapshot ContextSnapshot
	if err := msgpack.Unmarshal(payload, &snapshot); err != nil {
		return nil, fmt.Errorf("unmarshal context snapshot: %w", err)
	}
	if snapshot.SchemaVersion != ContextSnapshotSchemaVersion {
		return nil, fmt.Errorf("unsupported context snapshot schema version %d", snapshot.SchemaVersion)
	}
	expected := snapshot.Checksum
	snapshot.Checksum = ""
	unsigned, err := msgpack.Marshal(&snapshot)
	if err != nil {
		return nil, fmt.Errorf("verify context snapshot checksum: %w", err)
	}
	digest := sha256.Sum256(unsigned)
	if !strings.EqualFold(expected, hex.EncodeToString(digest[:])) {
		return nil, errors.New("context snapshot checksum mismatch")
	}
	snapshot.Checksum = expected
	if containsInlineImage(&snapshot) {
		return nil, errors.New("context snapshot contains inline image data")
	}
	return &snapshot, nil
}

func containsInlineImage(snapshot *ContextSnapshot) bool {
	if snapshot == nil {
		return false
	}
	for _, item := range snapshot.ModelInput {
		if strings.Contains(string(item.Raw), `"image_url":"data:`) {
			return true
		}
	}
	return false
}
