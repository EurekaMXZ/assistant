package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	assistantattachment "github.com/EurekaMXZ/assistant/internal/attachment"
	"github.com/EurekaMXZ/assistant/internal/domain"
)

const (
	sandboxFileReadTimeout        = 5 * time.Minute
	defaultSandboxFileReadLines   = 2000
	maxSandboxFileReadLines       = 2000
	maxSandboxFileReadLineLength  = 2000
	maxSandboxFileReadOutputBytes = 50 * 1024
	sandboxFileReadSampleBytes    = 4096
)

var sandboxReadableImageTypes = map[string]struct{}{
	"image/jpeg": {},
	"image/png":  {},
	"image/gif":  {},
	"image/webp": {},
}

type SandboxFileReadInput struct {
	ConversationID string
	TurnID         string
	OwnerUserID    string
	CallID         string
	Path           string
	Offset         int
	Limit          int
	RequestKey     string
}

type SandboxImageReference struct {
	AttachmentID string `json:"attachment_id"`
	ObjectKey    string `json:"object_key"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
}

type SandboxFileReadResult struct {
	Path        string                 `json:"path"`
	ContentType string                 `json:"content_type"`
	SizeBytes   int64                  `json:"size_bytes"`
	Content     string                 `json:"content,omitempty"`
	LineStart   int                    `json:"line_start,omitempty"`
	LineEnd     int                    `json:"line_end,omitempty"`
	Truncated   bool                   `json:"truncated,omitempty"`
	Image       *SandboxImageReference `json:"image,omitempty"`
}

type ReadSandboxFile struct {
	Attachments AssistantAttachmentStore
	Blobs       AssistantAttachmentBlobStore
	Sandboxes   ConversationSandboxStore
	Runtime     SandboxManager
	Files       SandboxFileReader
	Locker      ConversationLocker
}

func (uc ReadSandboxFile) Execute(ctx context.Context, input SandboxFileReadInput) (*SandboxFileReadResult, error) {
	if uc.Attachments == nil || uc.Blobs == nil || uc.Sandboxes == nil || uc.Runtime == nil || uc.Files == nil {
		return nil, errors.New("sandbox file read is not configured")
	}
	conversationID := strings.TrimSpace(input.ConversationID)
	if conversationID == "" {
		return nil, domain.NewValidationError("conversation id is required")
	}
	if strings.TrimSpace(input.TurnID) == "" {
		return nil, domain.NewValidationError("turn id is required")
	}
	if strings.TrimSpace(input.OwnerUserID) == "" {
		return nil, domain.NewValidationError("owner user id is required")
	}
	if strings.TrimSpace(input.CallID) == "" {
		return nil, domain.NewValidationError("tool call id is required")
	}
	sandboxPath, err := normalizeSandboxWorkspacePath(input.Path)
	if err != nil {
		return nil, err
	}
	if len(sandboxPath) > 4096 {
		return nil, domain.NewValidationError("path is too long")
	}
	offset := input.Offset
	if offset <= 0 {
		offset = 1
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultSandboxFileReadLines
	}
	if limit > maxSandboxFileReadLines {
		return nil, domain.NewValidationError(fmt.Sprintf("limit exceeds %d lines", maxSandboxFileReadLines))
	}

	operationCtx, cancel := context.WithTimeout(ctx, sandboxFileReadTimeout)
	defer cancel()
	return runConversationSandboxExecution(operationCtx, uc.Sandboxes, uc.Runtime, uc.Locker, conversationID, input.RequestKey, sandboxFileReadTimeout+sandboxExecutionLeaseBuffer, func(readCtx context.Context, handle domain.SandboxHandle) (*SandboxFileReadResult, error) {
		resolvedPath, err := resolveSandboxReadPath(readCtx, uc.Runtime, handle, sandboxPath, input.RequestKey)
		if err != nil {
			return nil, err
		}
		reader, size, err := uc.Files.ReadSandboxFile(readCtx, handle, resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("read sandbox file: %w", err)
		}
		if reader == nil {
			return nil, errors.New("read sandbox file returned no content")
		}
		defer reader.Close()
		if size < 0 || size > domain.SandboxFileMaxBytes {
			return nil, fmt.Errorf("sandbox file must be between 0 and %d bytes", domain.SandboxFileMaxBytes)
		}

		buffered := bufio.NewReaderSize(io.LimitReader(reader, size), sandboxFileReadSampleBytes)
		sample, err := sandboxFileSample(buffered, size)
		if err != nil {
			return nil, fmt.Errorf("sample sandbox file: %w", err)
		}
		contentType := assistantattachment.NormalizeContentType(path.Base(resolvedPath), http.DetectContentType(sample))
		if isSandboxReadableImage(contentType) {
			attachment, err := persistAssistantAttachment(readCtx, uc.Attachments, uc.Blobs, assistantAttachmentPersistInput{
				ConversationID: conversationID,
				TurnID:         input.TurnID,
				OwnerUserID:    input.OwnerUserID,
				CallID:         input.CallID,
				Filename:       path.Base(resolvedPath),
				ContentType:    contentType,
				Source:         "sandbox_read",
				SourcePath:     resolvedPath,
				Reader:         buffered,
				SizeBytes:      size,
			})
			if err != nil {
				return nil, fmt.Errorf("persist sandbox image: %w", err)
			}
			objectKey := attachment.ObjectKey
			if objectKey == "" {
				objectKey = assistantAttachmentObjectKey(conversationID, input.TurnID, input.CallID, path.Base(resolvedPath))
			}
			return &SandboxFileReadResult{
				Path:        resolvedPath,
				ContentType: attachment.ContentType,
				SizeBytes:   attachment.SizeBytes,
				Image: &SandboxImageReference{
					AttachmentID: attachment.ID,
					ObjectKey:    objectKey,
					ContentType:  attachment.ContentType,
					SizeBytes:    attachment.SizeBytes,
					SHA256:       attachment.SHA256,
				},
			}, nil
		}

		if isSandboxBinaryFile(resolvedPath, contentType, sample) {
			return nil, domain.NewValidationError("sandbox.read_file supports UTF-8 text and JPEG, PNG, GIF, or WebP images only")
		}
		content, lineStart, lineEnd, truncated, err := readSandboxText(buffered, resolvedPath, offset, limit)
		if err != nil {
			return nil, err
		}
		return &SandboxFileReadResult{
			Path:        resolvedPath,
			ContentType: "text/plain; charset=utf-8",
			SizeBytes:   size,
			Content:     content,
			LineStart:   lineStart,
			LineEnd:     lineEnd,
			Truncated:   truncated,
		}, nil
	})
}

func resolveSandboxReadPath(ctx context.Context, runtime SandboxManager, handle domain.SandboxHandle, value string, requestKey string) (string, error) {
	resolved, err := resolveSandboxExportPath(ctx, runtime, handle, value, requestKey+":resolve")
	if err != nil {
		return "", err
	}
	result, err := runtime.ExecSandboxCommand(ctx, handle, domain.SandboxCommandRequest{
		Command: "test", Args: []string{"-f", "--", resolved}, WorkingDirectory: "/workspace", TimeoutSeconds: 30,
	}, requestKey+":regular-file")
	if err != nil {
		return "", fmt.Errorf("verify sandbox file type: %w", err)
	}
	if result == nil || result.ExitCode != 0 {
		return "", domain.NewValidationError("sandbox read path must be a regular file")
	}
	return resolved, nil
}

func isSandboxReadableImage(contentType string) bool {
	_, ok := sandboxReadableImageTypes[strings.ToLower(strings.TrimSpace(contentType))]
	return ok
}

func sandboxFileSample(reader *bufio.Reader, size int64) ([]byte, error) {
	if size <= 0 {
		return nil, nil
	}
	sampleSize := int64(sandboxFileReadSampleBytes)
	if size < sampleSize {
		sampleSize = size
	}
	sample, err := reader.Peek(int(sampleSize))
	if err != nil && !errors.Is(err, bufio.ErrBufferFull) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return append([]byte(nil), sample...), nil
}

func isSandboxBinaryFile(filename string, contentType string, sample []byte) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(contentType, "text/") {
		return false
	}
	if contentType == "application/json" || contentType == "application/xml" {
		return false
	}
	if contentType == "application/octet-stream" && strings.Contains(strings.ToLower(path.Ext(filename)), "txt") {
		return false
	}
	if category := assistantattachment.ClassifyAttachment(contentType, path.Base(filename)); category == domain.AttachmentCategoryDocument || category == domain.AttachmentCategoryBinary {
		return true
	}
	if len(sample) == 0 {
		return false
	}
	if !utf8.Valid(sample) {
		return true
	}
	nonPrintable := 0
	for _, value := range sample {
		if value == 0 || value < 9 || (value > 13 && value < 32) {
			if value == 0 {
				return true
			}
			nonPrintable++
		}
	}
	return nonPrintable*100 > len(sample)*30
}

func readSandboxText(reader *bufio.Reader, filename string, offset int, limit int) (string, int, int, bool, error) {
	var lines []string
	lineNumber := 0
	lineCount := 0
	more := false
	cut := false
	for {
		line, err := readSandboxLine(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", 0, 0, false, fmt.Errorf("read text file %q: %w", filename, err)
		}
		lineNumber++
		lineCount = lineNumber
		if !utf8.ValidString(line) {
			return "", 0, 0, false, domain.NewValidationError("sandbox file must contain valid UTF-8 text")
		}
		if lineNumber < offset {
			continue
		}
		if len(lines) >= limit {
			more = true
			break
		}

		line = truncateSandboxLine(line)
		formatted := fmt.Sprintf("%d: %s", lineNumber, line)
		currentBytes := 0
		for _, existing := range lines {
			currentBytes += len(existing) + 1
		}
		if currentBytes+len(formatted) > maxSandboxFileReadOutputBytes {
			cut = true
			more = true
			break
		}
		lines = append(lines, formatted)
	}

	if lineCount < offset && !(lineCount == 0 && offset == 1) {
		return "", 0, 0, false, domain.NewValidationError(fmt.Sprintf("offset %d is out of range for this file", offset))
	}
	lineStart := offset
	lineEnd := offset + len(lines) - 1
	var builder strings.Builder
	builder.WriteString("<path>")
	builder.WriteString(filename)
	builder.WriteString("</path>\n<type>file</type>\n<content>\n")
	builder.WriteString(strings.Join(lines, "\n"))
	last := lineEnd
	if cut {
		fmt.Fprintf(&builder, "\n\n(Output capped at %d KiB. Use offset=%d to continue.)", maxSandboxFileReadOutputBytes/1024, max(1, last+1))
	} else if more {
		fmt.Fprintf(&builder, "\n\n(Showing lines %d-%d. Use offset=%d to continue.)", lineStart, last, max(1, last+1))
	} else {
		fmt.Fprintf(&builder, "\n\n(End of file - total %d lines)", lineCount)
	}
	builder.WriteString("\n</content>")
	return builder.String(), lineStart, lineEnd, cut || more, nil
}

func readSandboxLine(reader *bufio.Reader) (string, error) {
	var line []byte
	for {
		part, prefix, err := reader.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return strings.TrimSuffix(string(line), "\r"), nil
			}
			return "", err
		}
		line = append(line, part...)
		if !prefix {
			return strings.TrimSuffix(string(line), "\r"), nil
		}
	}
}

func truncateSandboxLine(line string) string {
	runes := []rune(line)
	if len(runes) <= maxSandboxFileReadLineLength {
		return line
	}
	return string(runes[:maxSandboxFileReadLineLength]) + "... (line truncated to 2000 chars)"
}

func SandboxReadFileImageReference(scope ToolScope, call ToolCall, output string) (*SandboxFileReadResult, error) {
	if normalizedToolName(call) != SandboxReadFile {
		return nil, nil
	}
	var payload struct {
		ConversationID string                 `json:"conversation_id"`
		TurnID         string                 `json:"turn_id"`
		File           *SandboxFileReadResult `json:"file"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, fmt.Errorf("decode sandbox.read_file output: %w", err)
	}
	if payload.File == nil || payload.File.Image == nil {
		return nil, nil
	}
	if payload.ConversationID != scope.ConversationID || payload.TurnID != scope.TurnID {
		return nil, errors.New("sandbox.read_file image reference has the wrong conversation scope")
	}
	if payload.File.Image.AttachmentID == "" || payload.File.Image.ObjectKey == "" || payload.File.Image.ContentType == "" {
		return nil, errors.New("sandbox.read_file image reference is incomplete")
	}
	if !isSandboxReadableImage(payload.File.Image.ContentType) {
		return nil, errors.New("sandbox.read_file image reference has an unsupported content type")
	}
	return payload.File, nil
}
