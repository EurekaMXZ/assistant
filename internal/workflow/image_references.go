package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"strings"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/llm"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type modelImageReference struct {
	AttachmentID string `json:"attachment_id"`
	ObjectKey    string `json:"object_key"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
}

const (
	maxProviderImageBytes      int64 = 20 << 20
	maxProviderTotalImageBytes int64 = 64 << 20
	maxProviderImageDimension        = 2048
	providerJPEGQuality              = 90
)

func cloneScheduledRunState(state *ScheduledRunState) (*ScheduledRunState, error) {
	if state == nil {
		return nil, nil
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("clone scheduled run state: %w", err)
	}
	var cloned ScheduledRunState
	if err := json.Unmarshal(payload, &cloned); err != nil {
		return nil, fmt.Errorf("clone scheduled run state: %w", err)
	}
	return &cloned, nil
}

func (l *ContextLoader) hydrateScheduledRunImages(ctx context.Context, state *ScheduledRunState) error {
	if state == nil {
		return nil
	}
	var totalBytes int64
	input := make([]llm.ModelItem, 0, len(state.Request.Input))
	for _, item := range state.Request.Input {
		generatedImage, handled, hydratedBytes, err := l.hydrateGeneratedImageCall(ctx, item)
		if err != nil {
			return err
		}
		totalBytes += hydratedBytes
		if totalBytes > maxProviderTotalImageBytes {
			return fmt.Errorf("provider request images exceed %d bytes", maxProviderTotalImageBytes)
		}
		if handled {
			if generatedImage != nil {
				input = append(input, *generatedImage)
			}
			continue
		}

		hydrated, hydratedBytes, err := l.hydrateModelItemImages(ctx, item)
		if err != nil {
			return err
		}
		totalBytes += hydratedBytes
		if totalBytes > maxProviderTotalImageBytes {
			return fmt.Errorf("provider request images exceed %d bytes", maxProviderTotalImageBytes)
		}
		input = append(input, hydrated)
	}
	state.Request.Input = input
	return nil
}

func (l *ContextLoader) hydrateGeneratedImageCall(ctx context.Context, item llm.ModelItem) (*llm.ModelItem, bool, int64, error) {
	if item.Type != llm.ModelItemImageGenerationCall {
		return nil, false, 0, nil
	}

	var payload struct {
		ResultRef *modelImageReference `json:"result_ref"`
	}
	if json.Unmarshal(item.Raw, &payload) != nil || payload.ResultRef == nil {
		return nil, true, 0, nil
	}

	data, contentType, err := l.loadProviderImageBytes(ctx, *payload.ResultRef)
	if err != nil {
		return nil, true, 0, err
	}
	raw, err := json.Marshal(map[string]any{
		"type": llm.ModelItemMessage,
		"role": domain.RoleUser,
		"content": []map[string]string{
			{"type": "input_text", "text": "Image generated earlier in this conversation."},
			{
				"type":      "input_image",
				"image_url": "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data),
			},
		},
	})
	if err != nil {
		return nil, true, 0, fmt.Errorf("marshal generated image input: %w", err)
	}
	return &llm.ModelItem{Type: llm.ModelItemMessage, Role: domain.RoleUser, Raw: raw}, true, int64(len(data)), nil
}

func (l *ContextLoader) hydrateModelItemImages(ctx context.Context, item llm.ModelItem) (llm.ModelItem, int64, error) {
	if len(item.Raw) == 0 {
		return item, 0, nil
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(item.Raw, &message); err != nil {
		return item, 0, nil
	}
	var content []json.RawMessage
	if err := json.Unmarshal(message["content"], &content); err != nil {
		return item, 0, nil
	}
	changed := false
	var hydratedBytes int64
	for index, rawPart := range content {
		var part struct {
			Type     string               `json:"type"`
			ImageRef *modelImageReference `json:"image_ref"`
		}
		if json.Unmarshal(rawPart, &part) != nil || part.Type != "input_image" || part.ImageRef == nil {
			continue
		}
		data, contentType, err := l.loadProviderImageBytes(ctx, *part.ImageRef)
		if err != nil {
			return item, 0, err
		}
		imageURL := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
		content[index], err = json.Marshal(map[string]string{"type": "input_image", "image_url": imageURL})
		if err != nil {
			return item, 0, fmt.Errorf("marshal hydrated image: %w", err)
		}
		hydratedBytes += int64(len(data))
		changed = true
	}
	if !changed {
		return item, 0, nil
	}
	encodedContent, err := json.Marshal(content)
	if err != nil {
		return item, 0, fmt.Errorf("marshal hydrated image content: %w", err)
	}
	message["content"] = encodedContent
	item.Raw, err = json.Marshal(message)
	if err != nil {
		return item, 0, fmt.Errorf("marshal hydrated model item: %w", err)
	}
	return item, hydratedBytes, nil
}

func (l *ContextLoader) hydrateImageReference(ctx context.Context, ref modelImageReference) (string, error) {
	data, contentType, err := l.loadProviderImageBytes(ctx, ref)
	if err != nil {
		return "", err
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (l *ContextLoader) hydrateImageReferenceBytes(ctx context.Context, ref modelImageReference) (string, error) {
	data, err := l.loadImageReferenceBytes(ctx, ref)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func (l *ContextLoader) loadImageReferenceBytes(ctx context.Context, ref modelImageReference) ([]byte, error) {
	if l == nil || l.attachmentBlobs == nil {
		return nil, fmt.Errorf("load image attachment %s: attachment blob store is not configured", ref.AttachmentID)
	}
	if ref.SizeBytes > maxProviderImageBytes {
		return nil, fmt.Errorf("load image attachment %s: image exceeds %d bytes", ref.AttachmentID, maxProviderImageBytes)
	}
	data, err := l.attachmentBlobs.GetBytes(ctx, ref.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("load image attachment %s: %w", ref.AttachmentID, err)
	}
	if int64(len(data)) > maxProviderImageBytes {
		return nil, fmt.Errorf("load image attachment %s: image exceeds %d bytes", ref.AttachmentID, maxProviderImageBytes)
	}
	if ref.SizeBytes > 0 && int64(len(data)) != ref.SizeBytes {
		return nil, fmt.Errorf("load image attachment %s: size mismatch", ref.AttachmentID)
	}
	if expected := strings.TrimSpace(ref.SHA256); expected != "" {
		digest := sha256.Sum256(data)
		if !strings.EqualFold(expected, hex.EncodeToString(digest[:])) {
			return nil, fmt.Errorf("load image attachment %s: checksum mismatch", ref.AttachmentID)
		}
	}
	return data, nil
}

func (l *ContextLoader) loadProviderImageBytes(ctx context.Context, ref modelImageReference) ([]byte, string, error) {
	data, err := l.loadImageReferenceBytes(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	data, contentType, err := downsampleProviderImage(data, ref.ContentType)
	if err != nil {
		return nil, "", fmt.Errorf("prepare image attachment %s for provider: %w", ref.AttachmentID, err)
	}
	return data, contentType, nil
}

func downsampleProviderImage(data []byte, contentType string) ([]byte, string, error) {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if contentType == "image/gif" {
		return data, contentType, nil
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("decode image config: %w", err)
	}
	orientation := jpegEXIFOrientation(data, contentType)
	if config.Width <= maxProviderImageDimension && config.Height <= maxProviderImageDimension && orientation == 1 {
		return data, contentType, nil
	}

	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}
	source = applyImageOrientation(source, orientation)
	width, height := source.Bounds().Dx(), source.Bounds().Dy()
	targetWidth, targetHeight := scaledDimensions(width, height, maxProviderImageDimension)
	if targetWidth != width || targetHeight != height {
		destination := image.NewNRGBA(image.Rect(0, 0, targetWidth, targetHeight))
		draw.CatmullRom.Scale(destination, destination.Bounds(), source, source.Bounds(), draw.Over, nil)
		source = destination
	}

	var output bytes.Buffer
	if contentType == "image/jpeg" || contentType == "image/jpg" {
		if err := jpeg.Encode(&output, source, &jpeg.Options{Quality: providerJPEGQuality}); err != nil {
			return nil, "", fmt.Errorf("encode jpeg: %w", err)
		}
		return output.Bytes(), "image/jpeg", nil
	}
	if err := png.Encode(&output, source); err != nil {
		return nil, "", fmt.Errorf("encode png: %w", err)
	}
	return output.Bytes(), "image/png", nil
}

func scaledDimensions(width int, height int, maximum int) (int, int) {
	if width <= maximum && height <= maximum {
		return width, height
	}
	if width >= height {
		return maximum, max(1, int(math.Round(float64(height)*float64(maximum)/float64(width))))
	}
	return max(1, int(math.Round(float64(width)*float64(maximum)/float64(height)))), maximum
}

func jpegEXIFOrientation(data []byte, contentType string) int {
	if (contentType != "image/jpeg" && contentType != "image/jpg") || len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xff {
			return 1
		}
		for offset < len(data) && data[offset] == 0xff {
			offset++
		}
		if offset >= len(data) {
			return 1
		}
		marker := data[offset]
		offset++
		if marker == 0xd9 || marker == 0xda {
			return 1
		}
		if marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if offset+2 > len(data) {
			return 1
		}
		segmentLength := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		if segmentLength < 2 || offset+segmentLength > len(data) {
			return 1
		}
		segment := data[offset+2 : offset+segmentLength]
		if marker == 0xe1 && len(segment) >= 14 && bytes.Equal(segment[:6], []byte("Exif\x00\x00")) {
			return tiffOrientation(segment[6:])
		}
		offset += segmentLength
	}
	return 1
}

func tiffOrientation(data []byte) int {
	if len(data) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(data[2:4]) != 42 {
		return 1
	}
	offset := int(order.Uint32(data[4:8]))
	if offset < 0 || offset+2 > len(data) {
		return 1
	}
	count := int(order.Uint16(data[offset : offset+2]))
	entries := offset + 2
	if entries+count*12 > len(data) {
		return 1
	}
	for index := 0; index < count; index++ {
		entry := entries + index*12
		if order.Uint16(data[entry:entry+2]) != 0x0112 || order.Uint16(data[entry+2:entry+4]) != 3 || order.Uint32(data[entry+4:entry+8]) != 1 {
			continue
		}
		orientation := int(order.Uint16(data[entry+8 : entry+10]))
		if orientation >= 1 && orientation <= 8 {
			return orientation
		}
	}
	return 1
}

func applyImageOrientation(source image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return source
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	destinationBounds := image.Rect(0, 0, width, height)
	if orientation >= 5 {
		destinationBounds = image.Rect(0, 0, height, width)
	}
	destination := image.NewNRGBA(destinationBounds)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			destinationX, destinationY := orientedPoint(x, y, width, height, orientation)
			destination.Set(destinationX, destinationY, source.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return destination
}

func orientedPoint(x int, y int, width int, height int, orientation int) (int, int) {
	switch orientation {
	case 2:
		return width - 1 - x, y
	case 3:
		return width - 1 - x, height - 1 - y
	case 4:
		return x, height - 1 - y
	case 5:
		return y, x
	case 6:
		return height - 1 - y, x
	case 7:
		return height - 1 - y, width - 1 - x
	case 8:
		return y, width - 1 - x
	default:
		return x, y
	}
}
