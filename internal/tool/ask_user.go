package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

const (
	AskUserKindSingleChoice   = "single_choice"
	AskUserKindExternalAction = "external_action"
	AskUserTonePrimary        = "primary"
	AskUserToneNeutral        = "neutral"
	AskUserToneDanger         = "danger"
)

var askUserOptionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type AskUserHandler struct{}

func (AskUserHandler) ToolName() string { return AskUser }

func (AskUserHandler) Execute(_ context.Context, _ ToolScope, call ToolCall) (*ToolExecutionResult, error) {
	prompt, err := decodeAskUserPrompt(call)
	if err != nil {
		return nil, err
	}
	prompt.CallID = call.CallID
	return &ToolExecutionResult{AwaitingInput: prompt}, nil
}

func decodeAskUserPrompt(call ToolCall) (*AskUserPrompt, error) {
	decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
	decoder.DisallowUnknownFields()
	var prompt AskUserPrompt
	if err := decoder.Decode(&prompt); err != nil {
		return nil, domain.NewValidationError("ask_user arguments are invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, domain.NewValidationError("ask_user arguments are invalid")
	}
	prompt.Prompt = strings.TrimSpace(prompt.Prompt)
	if prompt.Prompt == "" || !utf8.ValidString(prompt.Prompt) || utf8.RuneCountInString(prompt.Prompt) > 500 {
		return nil, domain.NewValidationError("ask_user prompt is invalid")
	}
	if len(prompt.Options) < 2 || len(prompt.Options) > 6 {
		return nil, domain.NewValidationError("ask_user requires 2 to 6 options")
	}
	seen := make(map[string]struct{}, len(prompt.Options))
	for index := range prompt.Options {
		option := &prompt.Options[index]
		option.ID = strings.TrimSpace(option.ID)
		option.Label = strings.TrimSpace(option.Label)
		if !askUserOptionIDPattern.MatchString(option.ID) {
			return nil, domain.NewValidationError("ask_user option id is invalid")
		}
		if _, duplicate := seen[option.ID]; duplicate {
			return nil, domain.NewValidationError("ask_user option ids must be unique")
		}
		seen[option.ID] = struct{}{}
		if option.Label == "" || !utf8.ValidString(option.Label) || utf8.RuneCountInString(option.Label) > 80 {
			return nil, domain.NewValidationError("ask_user option label is invalid")
		}
		switch option.Tone {
		case AskUserTonePrimary, AskUserToneNeutral, AskUserToneDanger:
		default:
			return nil, domain.NewValidationError("ask_user option tone is invalid")
		}
	}
	switch prompt.Kind {
	case AskUserKindSingleChoice:
		if prompt.Action != nil {
			return nil, domain.NewValidationError("single_choice ask_user must not include an action")
		}
	case AskUserKindExternalAction:
		if err := validateAskUserAction(prompt.Action); err != nil {
			return nil, err
		}
	default:
		return nil, domain.NewValidationError("ask_user kind is invalid")
	}
	return &prompt, nil
}

func DecodeAskUserPrompt(arguments json.RawMessage) (*AskUserPrompt, error) {
	return decodeAskUserPrompt(ToolCall{Arguments: arguments})
}

func validateAskUserAction(action *AskUserAction) error {
	if action == nil {
		return domain.NewValidationError("external_action ask_user requires an action")
	}
	action.Label = strings.TrimSpace(action.Label)
	action.URL = strings.TrimSpace(action.URL)
	if action.Label == "" || utf8.RuneCountInString(action.Label) > 80 || len(action.URL) > 2048 {
		return domain.NewValidationError("ask_user action is invalid")
	}
	parsed, err := url.Parse(action.URL)
	if err != nil || parsed.User != nil || parsed.Fragment != "" {
		return domain.NewValidationError("ask_user action URL is invalid")
	}
	switch parsed.Scheme {
	case "https":
		if parsed.Host == "" || unsafeAskUserHTTPSHost(parsed.Hostname()) {
			return domain.NewValidationError("ask_user action URL is invalid")
		}
	case "weixin":
		if !strings.EqualFold(parsed.Hostname(), "wap") || parsed.Port() != "" || parsed.EscapedPath() != "/pay" {
			return domain.NewValidationError("ask_user action URL is invalid")
		}
	default:
		return domain.NewValidationError("ask_user action URL must use https or weixin")
	}
	return nil
}

func unsafeAskUserHTTPSHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		host == "metadata" || strings.HasPrefix(host, "metadata.") || host == "instance-data" {
		return true
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		numeric := true
		for _, character := range host {
			if (character < '0' || character > '9') && character != '.' {
				numeric = false
				break
			}
		}
		if numeric || strings.HasPrefix(host, "0x") {
			return true
		}
		return false
	}
	address = address.Unmap()
	if address.IsPrivate() || address.IsLoopback() || address.IsUnspecified() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || !address.IsGlobalUnicast() {
		return true
	}
	for _, prefix := range []netip.Prefix{
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
	} {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
