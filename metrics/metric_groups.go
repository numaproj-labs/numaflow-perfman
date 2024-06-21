package metrics

// MetricGroups is a collection of MetricObjects organized by group. The group names, i.e. keys,
// are what will be provided via the CLI, and the corresponding values will be processed
var MetricGroups = map[string][]MetricObject{
	"throughput": {
		InboundMessages,
	},
	"latency": {
		ForwarderProcessingTimeP90,
		ForwarderProcessingTimeP95,
		ForwarderProcessingTimeP99,
	},
}
