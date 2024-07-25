package collect

import (
	"strings"
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

var (
	labelSet4 = map[model.LabelName]model.LabelValue{
		"replica":     "0",
		"vertex":      "output",
		"vertex_type": "Sink",
	}
	samplePair4 = []model.SamplePair{
		{
			Timestamp: 7890,
			Value:     3,
		},
	}
	testSampleStream4 = model.SampleStream{
		Metric: labelSet4,
		Values: samplePair4,
	}
)

var testMatrix1 model.Matrix = []*model.SampleStream{&testSampleStream1, &testSampleStream2, &testSampleStream3}
var testMatrix2 model.Matrix = []*model.SampleStream{&testSampleStream4}

func TestWriteToDumpFile(t *testing.T) {
	tests := []struct {
		name                string
		filename            string
		metricObject        metrics.MetricObject
		matrix              model.Matrix
		expectedFileContent string
		wantErr             bool
		errMessage          string
	}{
		{
			name:         "successfully create CSV file",
			filename:     "test1.csv",
			metricObject: testMetricObject,
			matrix:       testMatrix1,
			expectedFileContent: `unix_timestamp, bytes, pod, replica, vertex_type
1.23, 1, test-pod-input, 0, Source
4.56, 2, test-pod-map, 0, MapUDF
7.89, 3, test-pod-output, 0, Sink
`,
			wantErr:    false,
			errMessage: "",
		},
		{
			name:         "missing key in label set",
			filename:     "test2.csv",
			metricObject: testMetricObject,
			matrix:       testMatrix2,
			wantErr:      true,
			errMessage:   "label pod does not exist in the Metric map",
		},
	}
	memMapFS := afero.NewMemMapFs()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := memMapFS.Create(tt.filename)
			if err != nil {
				t.Fatalf("failed to create in-memory file: %v", err)
			}
			defer func() {
				if closeErr := f.Close(); closeErr != nil {
					t.Fatalf("failed to close in-memory file: %v", closeErr)
				}
			}()
			writeErr := writeToDumpFile(f, tt.metricObject, tt.matrix)

			fileContent, err := afero.ReadFile(memMapFS, tt.filename)
			if err != nil {
				t.Fatalf("failed to read in-memory file: %v", err)
			}
			if tt.wantErr {
				assert.Error(t, writeErr, "Expected error")
				assert.True(t, strings.Contains(writeErr.Error(), tt.errMessage))
			} else {
				assert.NoError(t, writeErr, "Expected no error")
				assert.Equal(t, tt.expectedFileContent, string(fileContent))
			}
		})
	}
}
