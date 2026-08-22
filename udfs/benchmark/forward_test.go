package benchmark

import (
	"testing"

	"github.com/numaproj/numaflow-go/pkg/batchmapper"
	"github.com/numaproj/numaflow-go/pkg/mapper"
	"github.com/numaproj/numaflow-go/pkg/mapstreamer"
	"github.com/numaproj/numaflow-go/pkg/sinker"
	"github.com/numaproj/numaflow-go/pkg/sourcetransformer"
)

func TestBenchmarkHandlersImplementSDKInterfaces(t *testing.T) {
	var _ mapper.Mapper = ForwardMap{}
	var _ batchmapper.BatchMapper = ForwardBatchMap{}
	var _ mapstreamer.MapStreamer = ForwardStreamMap{}
	var _ sourcetransformer.SourceTransformer = PassThroughTransformer{}
	var _ sinker.Sinker = BlackholeSink{}
}
