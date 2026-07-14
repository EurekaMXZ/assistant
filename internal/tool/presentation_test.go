package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildPublicToolPresentationSanitizesTavilySearch(t *testing.T) {
	presentation := BuildPublicToolPresentation(
		"internet",
		"",
		"search",
		"completed",
		json.RawMessage(`{"query":"latest release","api_key":"argument-secret"}`),
		[]byte(`{"answer":"private answer","results":[{"title":"private title","url":"HTTPS://user:password@www.Example.com/news?api_key=url-secret&lang=en#private","content":"private content"},{"url":"https://www.Example.com/news?lang=en"},{"url":"javascript:alert(1)"}],"token":"response-secret"}`),
		"private error",
	)

	if presentation.Title != "Searching the Web" || presentation.InputLabel != "Keywords" || presentation.InputText != "latest release" {
		t.Fatalf("unexpected search presentation: %#v", presentation)
	}
	if presentation.Summary != "Searched the web" || len(presentation.Details) != 2 || presentation.Details[0] != "Query: latest release" || presentation.Details[1] != "Results: 3" {
		t.Fatalf("unexpected trace fallback fields: %#v", presentation)
	}
	if len(presentation.Links) != 1 {
		t.Fatalf("links = %#v, want one sanitized, deduplicated link", presentation.Links)
	}
	link := presentation.Links[0]
	if link.URL != "https://www.Example.com/news?lang=en" || link.Label != "example.com" {
		t.Fatalf("unexpected sanitized link: %#v", link)
	}
	encoded, err := json.Marshal(presentation)
	if err != nil {
		t.Fatalf("marshal presentation: %v", err)
	}
	for _, secret := range []string{"argument-secret", "url-secret", "response-secret", "private answer", "private title", "private content", "private error", "password", "#private"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("presentation leaked %q: %s", secret, encoded)
		}
	}
}

func TestBuildPublicToolPresentationUsesInternetToolSpecificFields(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		arguments  string
		output     string
		title      string
		inputLabel string
		inputText  string
		linkURL    string
	}{
		{name: "extract", toolName: "extract", arguments: `{"urls":["https://docs.example.com/a#section"],"query":"release notes"}`, output: `{"results":[{"url":"https://docs.example.com/a"}]}`, title: "Reading Web Content", inputLabel: "Query", inputText: "release notes", linkURL: "https://docs.example.com/a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			presentation := BuildPublicToolPresentation("internet", "", tt.toolName, "completed", json.RawMessage(tt.arguments), []byte(tt.output), "")
			if presentation.Title != tt.title || presentation.InputLabel != tt.inputLabel || presentation.InputText != tt.inputText {
				t.Fatalf("unexpected presentation: %#v", presentation)
			}
			if tt.linkURL == "" {
				if len(presentation.Links) != 0 {
					t.Fatalf("unexpected links: %#v", presentation.Links)
				}
				return
			}
			if len(presentation.Links) == 0 || presentation.Links[0].URL != tt.linkURL {
				t.Fatalf("links = %#v, want first URL %q", presentation.Links, tt.linkURL)
			}
		})
	}
}

func TestBuildPublicToolPresentationDoesNotTreatContentAsLink(t *testing.T) {
	presentation := BuildPublicToolPresentation(
		"internet",
		"",
		"search",
		"completed",
		json.RawMessage(`{"query":"security"}`),
		[]byte(`{"results":[{"title":"https://title-secret.example","content":"https://content-secret.example"}]}`),
		"",
	)
	if len(presentation.Links) != 0 {
		t.Fatalf("non-URL result fields became public links: %#v", presentation.Links)
	}
}

func TestBuildPublicToolPresentationExposesSandboxCommandResult(t *testing.T) {
	presentation := BuildPublicToolPresentation(
		"",
		"",
		SandboxExec,
		"completed",
		json.RawMessage(`{"command":"bash","args":["-lc","printf 'hello\\n' && printf 'warning\\n' >&2"],"working_directory":"work dir"}`),
		[]byte(`{"conversation_id":"conv-1","result":{"runtime_id":"runtime-1","command":"bash","args":["-lc","printf 'hello\\n' && printf 'warning\\n' >&2"],"working_directory":"work dir","stdout":"hello\n","stderr":"warning\n","exit_code":7}}`),
		"",
	)

	if presentation.Title != "命令执行完成" {
		t.Fatalf("Title = %q", presentation.Title)
	}
	if presentation.Command != `bash -lc 'printf '"'"'hello\n'"'"' && printf '"'"'warning\n'"'"' >&2'` {
		t.Fatalf("Command = %q", presentation.Command)
	}
	if presentation.WorkingDirectory != "work dir" || presentation.Stdout != "hello\n" || presentation.Stderr != "warning\n" {
		t.Fatalf("unexpected command output: %#v", presentation)
	}
	if presentation.ExitCode == nil || *presentation.ExitCode != 7 {
		t.Fatalf("ExitCode = %#v", presentation.ExitCode)
	}
}
