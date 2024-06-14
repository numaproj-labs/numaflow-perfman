package collect

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"
)

func ProcessMetrics(prometheusAPI v1.API, metricObjects []MetricObject, timePeriod int, log *zap.Logger, inputOptions ...Option) error {
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
		// if there are any warnings, log them
		if len(warnings) > 0 {
			for _, w := range warnings {
				log.Warn("Prometheus API warning", zap.String("warning", w))
			}
		}

		matrix := result.(model.Matrix)
		for _, v := range matrix {
			fmt.Printf("Metadata: %v\n", v.Metric)
			fmt.Printf("%s\n", obj.Title)
			fmt.Printf("%s, %s\n", obj.XAxis, obj.YAxis)
			for _, val := range v.Values {
				fmt.Printf("%v, %v\n", val.Value, val.Timestamp)
			}

			fmt.Printf("\n")
		}
	}

	return nil
}
