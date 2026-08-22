package results

import "time"

// Run is a persisted benchmark or validation execution record.
type Run struct {
	ID                   string
	Kind                 string
	Scenario             string
	ImageRef             string
	ImageDigest          string
	Status               string
	Namespace            string
	CreatedAt            time.Time
	WarmupStartedAt      *time.Time
	MeasurementStartedAt *time.Time
	MeasurementEndedAt   *time.Time
	CompletedAt          *time.Time
	Seed                 *int64
	EventCount           *int64
	ManifestHash         string
	ConfigJSON           string
	EnvironmentJSON      string
	ErrorMessage         string
}

// CreateRunParams supplies caller-generated run identity and initial fields.
type CreateRunParams struct {
	ID              string
	Kind            string
	Scenario        string
	ImageRef        string
	ImageDigest     string
	Status          string
	Namespace       string
	CreatedAt       time.Time
	Seed            *int64
	EventCount      *int64
	ManifestHash    string
	ConfigJSON      string
	EnvironmentJSON string
}

// UpdateRunLifecycleParams updates status and optional lifecycle timestamps.
type UpdateRunLifecycleParams struct {
	Status               string
	WarmupStartedAt      *time.Time
	MeasurementStartedAt *time.Time
	MeasurementEndedAt   *time.Time
	CompletedAt          *time.Time
	ErrorMessage         string
}

// RunEvent is an append-only diagnostic event for a run.
type RunEvent struct {
	ID         int64
	RunID      string
	Timestamp  time.Time
	Level      string
	Phase      string
	Message    string
	DetailJSON string
}

// AddEventParams appends a run event.
type AddEventParams struct {
	RunID      string
	Timestamp  time.Time
	Level      string
	Phase      string
	Message    string
	DetailJSON string
}

// MetricPoint is one sample in a metric series.
type MetricPoint struct {
	Timestamp           time.Time `json:"timestamp"`
	ElapsedMilliseconds int64     `json:"elapsed_ms"`
	Value               float64   `json:"value"`
}

// MetricSeriesInput describes one series to persist with SaveMetrics.
type MetricSeriesInput struct {
	MetricName      string
	DisplayName     string
	Unit            string
	PrometheusQuery string
	LabelsJSON      string
	Points          []MetricPoint
}

// SaveMetricsParams persists all series for a run in one transaction.
type SaveMetricsParams struct {
	RunID           string
	RequiredMetrics []string
	Series          []MetricSeriesInput
}

// ValidationResult holds correctness check outcomes for a validation run.
type ValidationResult struct {
	RunID                  string
	Passed                 bool
	SourceCount            int64
	ExpectedCount          int64
	LogicalOutputCount     int64
	PhysicalDeliveryCount  int64
	DuplicateDeliveryCount int64
	DuplicateRate          float64
	MissingCount           int64
	UnexpectedCount        int64
	CorruptedCount         int64
	RoutingMismatchCount   int64
	ChildMismatchCount     int64
	DetailJSON             string
}

// ListRunsFilter selects runs from the repository.
type ListRunsFilter struct {
	Kind        string
	Scenario    string
	ImageRef    string
	ImageDigest string
	Status      string
	StatusIn    []string
	After       *time.Time
	Before      *time.Time
	Limit       int
	Offset      int
}

// LoadedMetricSeries is a series with all points ordered by elapsed time.
type LoadedMetricSeries struct {
	ID              int64
	RunID           string
	MetricName      string
	DisplayName     string
	Unit            string
	PrometheusQuery string
	LabelsJSON      string
	Points          []MetricPoint
}

// HarnessConfig is the singleton harness configuration persisted in SQLite.
type HarnessConfig struct {
	Context                  string
	Cluster                  string
	PrometheusURL            string
	CentralNamespace         string
	MonitoringNamespace      string
	ValidationNamespace      string
	LogFormat                string
	Verbose                  bool
	UDFImage                 string
	Image                    string
	TestedNumaflowMajorMinor string
	UpdatedAt                time.Time
}

// RunArtifact is one opaque blob associated with a run.
type RunArtifact struct {
	ID        int64
	RunID     string
	Kind      string
	Name      string
	MediaType string
	Content   []byte
	SizeBytes int64
	SHA256    string
	CreatedAt time.Time
}

// RunArtifactMeta describes a run artifact without loading its content.
type RunArtifactMeta struct {
	ID        int64     `json:"id"`
	RunID     string    `json:"run_id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	MediaType string    `json:"media_type"`
	SizeBytes int64     `json:"size_bytes"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
}

// PutRunArtifactParams stores or replaces one run artifact.
type PutRunArtifactParams struct {
	RunID     string
	Kind      string
	Name      string
	MediaType string
	Content   []byte
}

// ValidationFailureSample is one normalized validation defect sample.
type ValidationFailureSample struct {
	Ordinal    int    `json:"ordinal"`
	Kind       string `json:"kind"`
	LogicalKey string `json:"logical_key,omitempty"`
	Expected   string `json:"expected,omitempty"`
	Actual     string `json:"actual,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// Report kind values persisted in reports.kind.
const (
	ReportKindComparison = "comparison"
	ReportKindSingle     = "single"
)

// ReportRunRef links a report to an included run.
type ReportRunRef struct {
	RunID string
	Group string
}

// SaveReportParams persists a canonical report payload and metadata.
type SaveReportParams struct {
	ID                string
	Kind              string
	Scenario          string
	GeneratedAt       time.Time
	SelectionJSON     string
	Payload           []byte
	BaselineImageRef  string
	CandidateImageRef string
	RunRefs           []ReportRunRef
}

// StoredReport is a persisted report with payload and linked runs.
type StoredReport struct {
	ID                string
	Kind              string
	Scenario          string
	GeneratedAt       time.Time
	SelectionJSON     string
	Payload           []byte
	SizeBytes         int64
	SHA256            string
	BaselineImageRef  string
	CandidateImageRef string
	RunRefs           []ReportRunRef
}

// ReportSummary is report metadata without the payload blob.
type ReportSummary struct {
	ID                string
	Kind              string
	Scenario          string
	GeneratedAt       time.Time
	SizeBytes         int64
	SHA256            string
	BaselineImageRef  string
	CandidateImageRef string
}

// ListReportsFilter selects persisted reports.
type ListReportsFilter struct {
	Kind     string
	Scenario string
	Limit    int
	Offset   int
}

// ActiveLock is an exclusive database-backed lock row.
type ActiveLock struct {
	LockKey    string
	OwnerToken string
	PID        int
	Host       string
	Command    string
	RunID      string
	Namespace  string
	Scenario   string
	ImageRef   string
	ImageTag   string
	StartedAt  time.Time
	UpdatedAt  time.Time
}

// AcquireActiveLockParams creates a new active lock row.
type AcquireActiveLockParams struct {
	LockKey    string
	OwnerToken string
	PID        int
	Host       string
	Command    string
	RunID      string
	Namespace  string
	Scenario   string
	ImageRef   string
	ImageTag   string
	StartedAt  time.Time
}

// UpdateActiveLockParams updates mutable lock fields for the owner.
type UpdateActiveLockParams struct {
	PID       *int
	Host      *string
	Command   *string
	RunID     *string
	Namespace *string
	Scenario  *string
	ImageRef  *string
	ImageTag  *string
}
