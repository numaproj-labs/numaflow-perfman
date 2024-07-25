package collect

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"github.com/numaproj-labs/numaflow-perfman/metrics"
)

var testMetricObject = metrics.MetricObject{
	XAxis:  "unix_timestamp",
	YAxis:  "bytes",
	Labels: []string{"pod", "replica", "vertex_type"},
}

var (
	labelSet1 = map[model.LabelName]model.LabelValue{
		"pod":         "test-pod-input",
		"replica":     "0",
		"vertex_type": "Source",
	}
	samplePair1 = []model.SamplePair{
		{
			Timestamp: 1230,
			Value:     1,
		},
	}

	testSampleStream1 = model.SampleStream{
		Metric: labelSet1,
		Values: samplePair1,
	}
)

var (
	labelSet2 = map[model.LabelName]model.LabelValue{
		"pod":         "test-pod-map",
		"replica":     "0",
		"vertex_type": "MapUDF",
	}
	samplePair2 = []model.SamplePair{
		{
			Timestamp: 4560,
			Value:     2,
		},
	}
	testSampleStream2 = model.SampleStream{
		Metric: labelSet2,
		Values: samplePair2,
	}
)

var (
	labelSet3 = map[model.LabelName]model.LabelValue{
		"pod":         "test-pod-output",
		"replica":     "0",
		"vertex_type": "Sink",
	}
	samplePair3 = []model.SamplePair{
		{
			Timestamp: 7890,
			Value:     3,
		},
	}
	testSampleStream3 = model.SampleStream{
		Metric: labelSet3,
		Values: samplePair3,
	}
)

var testMatrix model.Matrix = []*model.SampleStream{&testSampleStream1, &testSampleStream2, &testSampleStream3}

func TestWriteToDumpFile(t *testing.T) {
	memMapFS := afero.NewMemMapFs()

	f, err := memMapFS.Create("test.csv")
	if err != nil {
		t.Fatalf("failed to create in-memory file: %v", err)
	}

	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("failed to close in-memory file: %v", closeErr)
		}
	}()

	err = writeToDumpFile(f, testMetricObject, testMatrix)
	if err != nil {
		t.Fatalf("failed to write to in-memory file: %v", err)
	}

	fileContent, err := afero.ReadFile(memMapFS, "test.csv")
	if err != nil {
		t.Fatalf("failed to read in-memory file: %v", err)
	}

	expectedContent := `unix_timestamp, bytes, pod, replica, vertex_type
1.23, 1, test-pod-input, 0, Source
4.56, 2, test-pod-map, 0, MapUDF
7.89, 3, test-pod-output, 0, Sink
`

	assert.Equal(t, expectedContent, string(fileContent))
}
