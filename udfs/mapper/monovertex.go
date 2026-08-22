package mapper

import (
	"context"
	"encoding/json"
	"log"

	"github.com/numaproj/numaflow-go/pkg/mapper"

	"numa-perfman/udfs/internal/event"
)

// MonoVertexMap enriches events with geo_location and fans out 0/1/2/3 children per
// input by event_type. The composite child_index is assembled from the transformer's
// stamped half (t) and the map's m index. Implements mapper.Mapper.
type MonoVertexMap struct{}

// NewMonoVertexMap constructs the MonoVertex map handler.
func NewMonoVertexMap() *MonoVertexMap { return &MonoVertexMap{} }

// Map enriches and fans out the event per the map fan-out rules.
func (h *MonoVertexMap) Map(_ context.Context, keys []string, datum mapper.Datum) mapper.Messages {
	value := datum.Value()
	if len(value) == 0 {
		return mapper.MessagesBuilder().Append(mapper.MessageToDrop())
	}
	in, err := decode(value)
	if err != nil {
		log.Printf("MonoVertexMap: failed to deserialize event: %v", err)
		return mapper.MessagesBuilder().Append(mapper.MessageToDrop())
	}

	t := in.TransformerChildIndex
	T := in.TransformerTotalChildren
	M := event.MapFanoutCount(in)
	if M == 0 {
		return mapper.MessagesBuilder().Append(mapper.MessageToDrop())
	}

	geo := event.DeterministicGeo(in.IpAddress)
	msgs := mapper.MessagesBuilder()
	for m := range M {
		child := in
		child.GeoLocation = &geo
		child.ChildIndex = event.MonoVertexChildIndex(t, m)
		child.TotalChildren = event.MonoVertexTotalChildren(T, M)
		payload, _ := json.Marshal(child)
		msgs = msgs.Append(mapper.NewMessage(payload).WithKeys(keys))
	}
	return msgs
}
