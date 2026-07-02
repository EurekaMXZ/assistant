package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewUsesServerSettings(t *testing.T) {
	srv := New(Settings{
		Address:      ":18080",
		WebOrigin:    "https://example.com",
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 3 * time.Second,
		IdleTimeout:  4 * time.Second,
	}, completeTestUseCases(UseCases{}), nil, context.Background())

	if srv.Addr != ":18080" {
		t.Fatalf("addr = %q, want %q", srv.Addr, ":18080")
	}
	if srv.ReadTimeout != 2*time.Second {
		t.Fatalf("read timeout = %v, want %v", srv.ReadTimeout, 2*time.Second)
	}
	if srv.WriteTimeout != 3*time.Second {
		t.Fatalf("write timeout = %v, want %v", srv.WriteTimeout, 3*time.Second)
	}
	if srv.IdleTimeout != 4*time.Second {
		t.Fatalf("idle timeout = %v, want %v", srv.IdleTimeout, 4*time.Second)
	}

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/healthz", nil)
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("allow-origin = %q, want %q", got, "https://example.com")
	}
}

func TestNewRejectsIncompleteUseCases(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New() did not reject incomplete use cases")
		}
	}()
	New(Settings{Address: ":0"}, UseCases{}, nil, context.Background())
}
