package upstream

import (
	"context"
	"io"
	"net/http"
)

type rawResponseKey struct{}
type RawResponseHandler func(http.ResponseWriter, io.Reader) error

// WithRawResponse opts trusted in-process adapters out of legacy SSE normalization.
func WithRawResponse(ctx context.Context, handler RawResponseHandler) context.Context {
	return context.WithValue(ctx, rawResponseKey{}, handler)
}
func RawResponse(ctx context.Context) RawResponseHandler {
	handler, _ := ctx.Value(rawResponseKey{}).(RawResponseHandler)
	return handler
}
