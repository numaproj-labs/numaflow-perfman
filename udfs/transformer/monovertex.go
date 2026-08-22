package transformer

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/numaproj/numaflow-go/pkg/sourcetransformer"

	"numa-perfman/udfs/internal/event"
)

// Tags the transformer attaches to bypass messages so the MonoVertex routes them
// straight to the onSuccess / fallback sinks (matching the pipeline YAML bypass tags).
const (
	bypassToOnSuccess = "bypass-to-onsuccess"
	bypassToFallback  = "bypass-to-fallback"
)

// MonoVertexTransformer reads the route-tag header, fans out 0/1/2/3 children per
// input by ad_type, and stamps transformer_child_index/total on each child so the
// downstream map (and bypass sinks) can compose the final composite child_index.
type MonoVertexTransformer struct{}

// NewMonoVertexTransformer constructs the MonoVertex transformer.
func NewMonoVertexTransformer() *MonoVertexTransformer { return &MonoVertexTransformer{} }

// Transform fans out the source event per the transformer fan-out rules.
func (t *MonoVertexTransformer) Transform(_ context.Context, keys []string, datum sourcetransformer.Datum) sourcetransformer.Messages {
	routeTag, ok := datum.Headers()[routeTagHeader]
	if !ok {
		log.Printf("MonoVertexTransformer: no route-tag header found, dropping message")
		return sourcetransformer.MessagesBuilder().Append(sourcetransformer.MessageToDrop(datum.EventTime()))
	}

	value := datum.Value()
	if len(value) == 0 {
		log.Printf("MonoVertexTransformer: empty payload, dropping")
		return sourcetransformer.MessagesBuilder().Append(sourcetransformer.MessageToDrop(datum.EventTime()))
	}

	var ev event.Event
	if err := json.Unmarshal(value, &ev); err != nil {
		log.Printf("MonoVertexTransformer: failed to deserialize event: %v", err)
		return sourcetransformer.MessagesBuilder().Append(sourcetransformer.MessageToDrop(datum.EventTime()))
	}

	T := event.TransformerFanoutCount(ev)
	if T == 0 {
		return sourcetransformer.MessagesBuilder().Append(sourcetransformer.MessageToDrop(datum.EventTime()))
	}

	var tags []string
	switch {
	case strings.HasPrefix(routeTag, "bypass-onsuccess"):
		tags = []string{bypassToOnSuccess}
	case strings.HasPrefix(routeTag, "bypass-fallback"):
		tags = []string{bypassToFallback}
	}

	msgs := sourcetransformer.MessagesBuilder()
	for tIdx := range T {
		child := ev
		child.TransformerChildIndex = tIdx
		child.TransformerTotalChildren = T
		payload, _ := json.Marshal(child)
		msg := sourcetransformer.NewMessage(payload, datum.EventTime()).WithKeys(keys)
		if tags != nil {
			msg = msg.WithTags(tags)
		}
		msgs = msgs.Append(msg)
	}
	return msgs
}
