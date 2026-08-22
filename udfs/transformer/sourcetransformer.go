// Package transformer implements the Numaflow source transformer handlers:
// the map-validations route-tag transformer and the MonoVertex transformer.
package transformer

import (
	"context"
	"log"

	"github.com/numaproj/numaflow-go/pkg/sourcetransformer"
)

const routeTagHeader = "route-tag"

// SourceTransformer reads the "route-tag" header set by the source and converts it
// into a Numaflow tag for conditional forwarding to the correct map UDF.
type SourceTransformer struct{}

// NewSourceTransformer constructs the route-tag transformer.
func NewSourceTransformer() *SourceTransformer { return &SourceTransformer{} }

// Transform passes the payload through unchanged, attaching the route tag. Messages
// without a route-tag header are dropped.
func (t *SourceTransformer) Transform(_ context.Context, keys []string, datum sourcetransformer.Datum) sourcetransformer.Messages {
	routeTag, ok := datum.Headers()[routeTagHeader]
	if !ok {
		log.Printf("SourceTransformer: no route-tag header found, dropping message")
		return sourcetransformer.MessagesBuilder().Append(sourcetransformer.MessageToDrop(datum.EventTime()))
	}

	msg := sourcetransformer.NewMessage(datum.Value(), datum.EventTime()).
		WithKeys(keys).
		WithTags([]string{routeTag})
	return sourcetransformer.MessagesBuilder().Append(msg)
}
