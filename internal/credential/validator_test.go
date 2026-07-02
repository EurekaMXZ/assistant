package credential

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidatorSendsCredentialWithoutLeakingProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-sensitive" {
			t.Errorf("Authorization = %q", got)
		}
		http.Error(w, `{"error":{"message":"rejected sk-sensitive"}}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	err := NewValidator(time.Second).Validate(t.Context(), server.URL, "sk-sensitive")
	if err == nil {
		t.Fatal("validation succeeded")
	}
	if strings.Contains(err.Error(), "sk-sensitive") {
		t.Fatalf("validation error leaked credential: %v", err)
	}
}
