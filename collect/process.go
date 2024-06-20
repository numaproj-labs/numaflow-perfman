package collect

import (
	"context"
	"fmt"
	"os"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"
)

func createDumpFilePath(reportDir string, metric string, filename string, timePeriod int) (*os.File, error) {
	err := os.MkdirAll(fmt.Sprintf("output/%s/%s", reportDir, metric), os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("failed to create report directory: %w", err)
	}

	f, err := os.Create(fmt.Sprintf("output/%s/%s/%s-%dmin.csv", reportDir, metric, filename, timePeriod))
	if err != nil {
		return nil, fmt.Errorf("failed to create dump file: %w", err)
	}

	return f, nil
}

// ProcessMetrics queries the Prometheus API with the given metric object and outputs the returned data into csv files
func ProcessMetrics(prometheusAPI v1.API, metric string, metricObjects []MetricObject, reportDir string, timePeriod int, log *zap.Logger, inputOptions ...Option) error {
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
		dumpFile, err := createDumpFilePath(reportDir, metric, obj.Filename, timePeriod)
		if err != nil {
			return fmt.Errorf("error creating data dump path: %w", err)
		}
		defer dumpFile.Close()

		if _, err := fmt.Fprintf(dumpFile, "%s, %s, Vertex, Vertex Type\n", obj.XAxis, obj.YAxis); err != nil {
			return fmt.Errorf("failed to write to file: %w", err)
		}

		result, warnings, err := prometheusAPI.QueryRange(context.TODO(), obj.Query, queryRange)
		if err != nil {
			return fmt.Errorf("error querying Prometheus: %w", err)
		}
		// if there are any warnings, log them
		if len(warnings) > 0 {
			for _, w := range warnings {
				log.Warn("Prometheus API warning", zap.String("warning", w))
			}
		}

		matrix := result.(model.Matrix)

		for _, v := range matrix {
			for _, val := range v.Values {
				if _, err := fmt.Fprintf(dumpFile, "%v, %v, %s, %s\n",
					val.Timestamp,
					val.Value,
					v.Metric["vertex"],
					v.Metric["vertex_type"]); err != nil {
					return fmt.Errorf("failed to write to file: %w", err)
				}
			}
		}
	}

	return nil
}
