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

func TestBuildPublicToolPresentationRecognizesTavilyNamespaceAliases(t *testing.T) {
	tests := []struct {
		name  string
		title string
	}{
		{name: "search", title: "Searching the Web"},
		{name: "extract", title: "Reading Web Content"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			presentation := BuildPublicToolPresentation(
				"tavily",
				"",
				test.name,
				"completed",
				json.RawMessage(`{"query":"docs","urls":["https://example.com"]}`),
				[]byte(`{"results":[]}`),
				"",
			)
			if presentation.Title != test.title || strings.HasPrefix(presentation.Summary, "Used ") {
				t.Fatalf("unexpected Tavily alias presentation: %#v", presentation)
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
		[]byte(`{"conversation_id":"conv-1","result":{"runtime_id":"runtime-1","command":"bash","args":["-lc","printf 'hello\\n' && printf 'warning\\n' >&2"],"working_directory":"work dir","output":"hello\nwarning\n","exit_code":7}}`),
		"",
	)

	if presentation.Title != "命令执行完成" {
		t.Fatalf("Title = %q", presentation.Title)
	}
	if presentation.Command != `bash -lc 'printf '"'"'hello\n'"'"' && printf '"'"'warning\n'"'"' >&2'` {
		t.Fatalf("Command = %q", presentation.Command)
	}
	if presentation.WorkingDirectory != "work dir" || presentation.CommandOutput != "hello\nwarning\n" {
		t.Fatalf("unexpected command output: %#v", presentation)
	}
	if presentation.ExitCode == nil || *presentation.ExitCode != 7 {
		t.Fatalf("ExitCode = %#v", presentation.ExitCode)
	}

}

func TestBuildPublicToolPresentationExposesImportedAttachmentPath(t *testing.T) {
	presentation := BuildPublicToolPresentation(
		"",
		"",
		SandboxImportAttachment,
		"completed",
		json.RawMessage(`{"attachment_id":"11111111-1111-4111-8111-111111111111"}`),
		[]byte(`{"attachment":{"filename":"report.csv","sandbox_path":"/workspace/attachment-11111111-1111-4111-8111-111111111111.csv"}}`),
		"",
	)
	if presentation.Title != "附件已导入沙箱" || presentation.InputText != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("unexpected attachment presentation: %#v", presentation)
	}
	details := strings.Join(presentation.Details, "\n")
	if !strings.Contains(details, "report.csv") || !strings.Contains(details, "/workspace/attachment-") {
		t.Fatalf("attachment details = %q", presentation.Details)
	}
}

func TestBuildPublicToolPresentationHidesSandboxWriteContent(t *testing.T) {
	presentation := BuildPublicToolPresentation(
		"",
		"",
		SandboxWriteFile,
		"completed",
		json.RawMessage(`{"path":"reports/result.md","content":"private generated content"}`),
		[]byte(`{"conversation_id":"conv-1","file":{"path":"/workspace/reports/result.md","size_bytes":25,"sha256":"abc123"}}`),
		"",
	)
	encoded, err := json.Marshal(presentation)
	if err != nil {
		t.Fatalf("marshal presentation: %v", err)
	}
	if presentation.Title != "文件已写入沙箱" || presentation.InputText != "reports/result.md" {
		t.Fatalf("unexpected write presentation: %#v", presentation)
	}
	if details := strings.Join(presentation.Details, "\n"); !strings.Contains(details, "/workspace/reports/result.md") || !strings.Contains(details, "25") {
		t.Fatalf("write details = %q", presentation.Details)
	}
	if strings.Contains(string(encoded), "private generated content") {
		t.Fatalf("write presentation leaked content: %s", encoded)
	}
}

func TestBuildPublicToolPresentationHidesSandboxEditText(t *testing.T) {
	presentation := BuildPublicToolPresentation(
		"",
		"",
		SandboxEditFile,
		"completed",
		json.RawMessage(`{"path":"reports/result.md","old_text":"private old text","new_text":"private new text","replace_all":false}`),
		[]byte(`{"conversation_id":"conv-1","file":{"path":"/workspace/reports/result.md","size_bytes":42,"sha256":"abc123","replacements":1}}`),
		"",
	)
	encoded, err := json.Marshal(presentation)
	if err != nil {
		t.Fatalf("marshal presentation: %v", err)
	}
	if presentation.Title != "沙箱文件已修改" || presentation.InputText != "reports/result.md" {
		t.Fatalf("unexpected edit presentation: %#v", presentation)
	}
	if details := strings.Join(presentation.Details, "\n"); !strings.Contains(details, "/workspace/reports/result.md") || !strings.Contains(details, "Replacements: 1") {
		t.Fatalf("edit details = %q", presentation.Details)
	}
	if strings.Contains(string(encoded), "private old text") || strings.Contains(string(encoded), "private new text") {
		t.Fatalf("edit presentation leaked replacement text: %s", encoded)
	}
}

func TestBuildPublicToolPresentationExposesPersistentShellResult(t *testing.T) {
	presentation := BuildPublicToolPresentation(
		"",
		"",
		SandboxShellConnect,
		"completed",
		json.RawMessage(`{"session_id":"shell-1","command":"pwd","timeout_seconds":30}`),
		[]byte(`{"conversation_id":"conv-1","result":{"runtime_id":"runtime-1","session_id":"shell-1","output":"/workspace/project\n","exit_code":0}}`),
		"",
	)
	if presentation.Title != "Shell 命令执行完成" || presentation.InputText != "shell-1" || presentation.Command != "pwd" {
		t.Fatalf("unexpected shell presentation: %#v", presentation)
	}
	if presentation.CommandOutput != "/workspace/project\n" || presentation.ExitCode == nil || *presentation.ExitCode != 0 {
		t.Fatalf("unexpected shell output presentation: %#v", presentation)
	}
}
