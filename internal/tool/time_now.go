package tool

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type TimeNowHandler struct {
	Now func() time.Time
}

func (TimeNowHandler) ToolName() string { return TimeNow }

func (h TimeNowHandler) Execute(_ context.Context, _ ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	var input struct {
		Timezone string `json:"timezone"`
	}
	if err := decodeToolArguments(call, TimeNow, &input); err != nil {
		return nil, err
	}

	location, err := timeNowLocation(input.Timezone)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	now = now.In(location)
	payload, err := marshalToolOutput(TimeNow, map[string]any{
		"timezone":     location.String(),
		"iso8601":      now.Format(time.RFC3339Nano),
		"unix_seconds": now.Unix(),
	})
	if err != nil {
		return nil, err
	}

	return outputOnlyExecutionResult(call.CallID, payload), nil
}

func timeNowLocation(timezone string) (*time.Location, error) {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		return time.UTC, nil
	}

	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("%s timezone %q is invalid: %w", TimeNow, timezone, err)
	}
	return location, nil
}
