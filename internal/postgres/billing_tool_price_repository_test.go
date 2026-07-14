package postgres

import (
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestBillingToolKey(t *testing.T) {
	tests := []struct {
		namespace string
		name      string
		want      string
	}{
		{namespace: "sandbox", name: "create", want: domain.BillingToolSandboxCreate},
		{namespace: "internet", name: "search", want: domain.BillingToolTavilySearch},
		{namespace: "internet", name: "extract", want: domain.BillingToolTavilyExtract},
		{namespace: "tavily", name: "search", want: domain.BillingToolTavilySearch},
		{namespace: "sandbox", name: "exec", want: ""},
	}
	for _, test := range tests {
		if got := billingToolKey(test.namespace, test.name); got != test.want {
			t.Errorf("billingToolKey(%q, %q) = %q, want %q", test.namespace, test.name, got, test.want)
		}
	}
}
