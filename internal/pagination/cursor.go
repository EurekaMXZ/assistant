package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

type Cursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func Encode(createdAt time.Time, id string) string {
	raw, _ := json.Marshal(Cursor{CreatedAt: createdAt.UTC(), ID: id})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func Decode(value string) (*Cursor, error) {
	if value == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("invalid cursor")
	}
	var cursor Cursor
	if err := json.Unmarshal(raw, &cursor); err != nil || cursor.CreatedAt.IsZero() || cursor.ID == "" {
		return nil, errors.New("invalid cursor")
	}
	return &cursor, nil
}
