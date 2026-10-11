package protocol

import (
	"context"
	"encoding/base64"
	"errors"
	"mime"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func init() {
	relayconvert.SetMediaResolver(relayconvert.MediaResolver{GetBase64Data: resolveInlineMedia})
}

// Resolution is deliberately in-memory: no network, Files service or persistent cache.
func resolveInlineMedia(ctx context.Context, source types.FileSource, _ ...string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if source == nil || source.IsURL() {
		return "", "", errors.New("only inline text/PDF media is supported")
	}
	data := source.GetRawData()
	if len(data) > maxResponseBytes {
		return "", "", errors.New("inline media exceeds limit")
	}
	mediaType := ""
	if b, ok := source.(*types.Base64Source); ok {
		mediaType = b.MimeType
	}
	if strings.HasPrefix(data, "data:") {
		header, value, ok := strings.Cut(strings.TrimPrefix(data, "data:"), ",")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return "", "", errors.New("inline media requires a base64 data URL")
		}
		mediaType = strings.TrimSuffix(header, ";base64")
		data = value
	}
	mediaType, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return "", "", errors.New("inline media requires a valid MIME type")
	}
	isText := strings.HasPrefix(mediaType, "text/") || mediaType == "application/json"
	if !isText && mediaType != "application/pdf" {
		return "", "", errors.New("only inline text/PDF media is supported")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(data)
	if err != nil || (isText && !utf8.Valid(decoded)) {
		return "", "", errors.New("invalid inline media encoding")
	}
	return data, mediaType, nil
}
