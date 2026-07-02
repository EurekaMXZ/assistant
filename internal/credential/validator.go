package credential

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Validator struct {
	client *http.Client
}

func NewValidator(timeout time.Duration) *Validator {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Validator{client: &http.Client{Timeout: timeout}}
}

func (v *Validator) Validate(ctx context.Context, baseURL string, apiKey string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(strings.TrimSpace(baseURL), "/")+"/models", nil)
	if err != nil {
		return fmt.Errorf("create provider credential validation request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")
	response, err := v.client.Do(request)
	if err != nil {
		return fmt.Errorf("provider credential validation request failed")
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("provider rejected credential: %s", http.StatusText(response.StatusCode))
	}
	return nil
}
