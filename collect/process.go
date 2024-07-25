package collect

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"

	"github.com/numaproj-labs/numaflow-perfman/metrics"
)

func createDumpFilePath(dataDir string, metric string, filename string, timePeriod int) (*os.File, error) {
	if err := os.MkdirAll(fmt.Sprintf("output/%s/%s", dataDir, metric), os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	f, err := os.Create(fmt.Sprintf("output/%s/%s/%s-%dmin.csv", dataDir, metric, filename, timePeriod))
	if err != nil {
		return nil, fmt.Errorf("failed to create dump file: %w", err)
	}

	return f, nil
}

func writeToDumpFile(dumpFile io.Writer, metricObject metrics.MetricObject, matrix model.Matrix) error {
	// Write the columns of the CSV file
	if _, err := fmt.Fprintf(dumpFile, "%s, %s, %s\n", metricObject.XAxis, metricObject.YAxis, strings.Join(metricObject.Labels, ", ")); err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	for _, ss := range matrix {
		for _, sp := range ss.Values {
			format := "%v, %v, " + strings.Repeat("%s, ", len(metricObject.Labels)-1) + "%s\n"
			args := []interface{}{sp.Timestamp, sp.Value}
			for _, label := range metricObject.Labels {
				args = append(args, ss.Metric[model.LabelName(label)])
			}

			// With each iteration over the result matrix, write one row of values
			if _, err := fmt.Fprintf(dumpFile, format, args...); err != nil {
				return fmt.Errorf("failed to write to file: %w", err)
			}
		}
	}

	return nil
}

// ProcessMetrics queries the Prometheus API with the given metric object and outputs the returned data into CSV files
// prometheusAPI: the Prometheus client used to make API calls with
// metric: the current metric group being processed
// dataDir: the name of the directory that the CSV files will be written to
// timePeriod: the time, in minutes, to look back, starting from the current time, for the Prometheus query
func ProcessMetrics(prometheusAPI v1.API, metric string, metricObjects []metrics.MetricObject, dataDir string, timePeriod int, log *zap.Logger, inputOptions ...Option) error {
	opts := DefaultOptions()

	for _, inputOption := range inputOptions {
		inputOption(opts)
	}

	queryRange := v1.Range{
		Start: time.Now().Add(time.Duration(-timePeriod) * time.Minute),
		End:   time.Now(),
		Step:  opts.step,
	}

	for _, obj := range metricObjects {
		result, warnings, err := prometheusAPI.QueryRange(context.TODO(), obj.Query, queryRange)
		if err != nil {
			return fmt.Errorf("error querying Prometheus: %w", err)
		}
		// If there are any warnings, log them
		if len(warnings) > 0 {
			for _, w := range warnings {
				log.Warn("Prometheus API warning", zap.String("warning", w))
			}
		}

		matrix := result.(model.Matrix)

		dumpFile, err := createDumpFilePath(dataDir, metric, obj.Filename, timePeriod)
		if err != nil {
			return fmt.Errorf("error creating dump file path: %w", err)
		}

		if err = writeToDumpFile(dumpFile, obj, matrix); err != nil {
			dumpFile.Close()
			return fmt.Errorf("error when processing metric object: %w", err)
		}

		// https://www.joeshaw.org/dont-defer-close-on-writable-files/
		if err = dumpFile.Close(); err != nil {
			return fmt.Errorf("error closing dump file: %w", err)
		}
	}

	return nil
}
