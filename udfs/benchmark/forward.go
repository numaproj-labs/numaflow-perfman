// Package benchmark provides lightweight UDF modes for perf scenarios (not correctness validation).
package benchmark

import (
	"context"

	"github.com/numaproj/numaflow-go/pkg/batchmapper"
	"github.com/numaproj/numaflow-go/pkg/mapper"
	"github.com/numaproj/numaflow-go/pkg/mapstreamer"
	"github.com/numaproj/numaflow-go/pkg/sinker"
	"github.com/numaproj/numaflow-go/pkg/sourcetransformer"
)

// ForwardMap passes each input message through unchanged.
type ForwardMap struct{}

func (ForwardMap) Map(_ context.Context, keys []string, datum mapper.Datum) mapper.Messages {
	return mapper.MessagesBuilder().Append(mapper.NewMessage(datum.Value()).WithKeys(keys))
}

// ForwardBatchMap passes each batch datum through unchanged.
type ForwardBatchMap struct{}

func (ForwardBatchMap) BatchMap(_ context.Context, datumStreamCh <-chan batchmapper.Datum) batchmapper.BatchResponses {
	responses := batchmapper.BatchResponsesBuilder()
	for datum := range datumStreamCh {
		resp := batchmapper.NewBatchResponse(datum.Id())
		resp = resp.Append(batchmapper.NewMessage(datum.Value()).WithKeys(datum.Keys()))
		responses = responses.Append(resp)
	}
	return responses
}

// ForwardStreamMap passes each stream datum through unchanged.
type ForwardStreamMap struct{}

func (ForwardStreamMap) MapStream(_ context.Context, _ []string, datum mapstreamer.Datum, messageCh chan<- mapstreamer.Message) {
	messageCh <- mapstreamer.NewMessage(datum.Value())
}

// PassThroughTransformer forwards source messages without modification.
type PassThroughTransformer struct{}

func (PassThroughTransformer) Transform(_ context.Context, keys []string, datum sourcetransformer.Datum) sourcetransformer.Messages {
	msg := sourcetransformer.NewMessage(datum.Value(), datum.EventTime()).WithKeys(keys)
	return sourcetransformer.MessagesBuilder().Append(msg)
}

// BlackholeSink discards all sink input.
type BlackholeSink struct{}

func (BlackholeSink) Sink(_ context.Context, datumStreamCh <-chan sinker.Datum) sinker.Responses {
	responses := sinker.ResponsesBuilder()
	for datum := range datumStreamCh {
		responses = responses.Append(sinker.ResponseOK(datum.ID()))
	}
	return responses
}
