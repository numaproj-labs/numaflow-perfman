// Package mapper implements the Numaflow map UDFs for the map-validations pipeline:
// unary map, batch map, stream map, and the MonoVertex map. Each map enriches events
// using the shared deterministic logic in internal/event.
package mapper

import (
	"context"
	"encoding/json"
	"log"

	"github.com/numaproj/numaflow-go/pkg/batchmapper"
	"github.com/numaproj/numaflow-go/pkg/mapper"
	"github.com/numaproj/numaflow-go/pkg/mapstreamer"

	"numa-perfman/udfs/internal/event"
)

// UnaryMap enriches events with geo_location. Implements mapper.Mapper.
type UnaryMap struct{}

// NewUnaryMap constructs the unary map handler.
func NewUnaryMap() *UnaryMap { return &UnaryMap{} }

// Map decodes the event, enriches it, and returns the fan-out outputs.
func (h *UnaryMap) Map(_ context.Context, keys []string, datum mapper.Datum) mapper.Messages {
	in, err := decode(datum.Value())
	if err != nil {
		log.Printf("UnaryMap: failed to deserialize event: %v", err)
		return mapper.MessagesBuilder().Append(mapper.MessageToDrop())
	}
	msgs := mapper.MessagesBuilder()
	for _, out := range event.ProduceOutputs(in, event.RouteTagUnaryMap) {
		payload, _ := json.Marshal(out)
		msgs = msgs.Append(mapper.NewMessage(payload).WithKeys(keys))
	}
	return msgs
}

// StreamMap enriches events with risk_score, streaming outputs one at a time.
// Implements mapstreamer.MapStreamer.
type StreamMap struct{}

// NewStreamMap constructs the stream map handler.
func NewStreamMap() *StreamMap { return &StreamMap{} }

// MapStream decodes the event, enriches it, and sends each output to the channel.
func (h *StreamMap) MapStream(_ context.Context, _ []string, datum mapstreamer.Datum, messageCh chan<- mapstreamer.Message) {
	in, err := decode(datum.Value())
	if err != nil {
		log.Printf("StreamMap: failed to deserialize event: %v", err)
		messageCh <- mapstreamer.MessageToDrop()
		return
	}
	for _, out := range event.ProduceOutputs(in, event.RouteTagStreamMap) {
		payload, _ := json.Marshal(out)
		messageCh <- mapstreamer.NewMessage(payload)
	}
}

// BatchMap enriches events with campaign_id, processing a batch at once.
// Implements batchmapper.BatchMapper.
type BatchMap struct{}

// NewBatchMap constructs the batch map handler.
func NewBatchMap() *BatchMap { return &BatchMap{} }

// BatchMap reads the datum stream and produces a response per input id.
func (h *BatchMap) BatchMap(_ context.Context, datumStreamCh <-chan batchmapper.Datum) batchmapper.BatchResponses {
	responses := batchmapper.BatchResponsesBuilder()
	for datum := range datumStreamCh {
		resp := batchmapper.NewBatchResponse(datum.Id())
		in, err := decode(datum.Value())
		if err != nil {
			log.Printf("BatchMap: failed to deserialize event: %v", err)
			resp = resp.Append(batchmapper.MessageToDrop())
			responses = responses.Append(resp)
			continue
		}
		for _, out := range event.ProduceOutputs(in, event.RouteTagBatchMap) {
			payload, _ := json.Marshal(out)
			resp = resp.Append(batchmapper.NewMessage(payload).WithKeys(datum.Keys()))
		}
		responses = responses.Append(resp)
	}
	return responses
}

func decode(value []byte) (event.Event, error) {
	var ev event.Event
	err := json.Unmarshal(value, &ev)
	return ev, err
}
