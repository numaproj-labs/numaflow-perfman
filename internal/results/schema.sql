-- Final run-centric results schema. Existing result stores are intentionally unsupported.

CREATE TABLE IF NOT EXISTS runs (
    id                      TEXT PRIMARY KEY,
    kind                    TEXT NOT NULL CHECK (kind IN ('benchmark', 'validation')),
    scenario                TEXT NOT NULL,
    image_ref               TEXT NOT NULL,
    image_digest            TEXT NOT NULL DEFAULT '',
    status                  TEXT NOT NULL,
    namespace               TEXT NOT NULL DEFAULT '',
    created_at              TEXT NOT NULL,
    warmup_started_at       TEXT,
    measurement_started_at  TEXT,
    measurement_ended_at    TEXT,
    completed_at            TEXT,
    seed                    INTEGER,
    event_count             INTEGER,
    manifest_hash           TEXT NOT NULL DEFAULT '',
    config_json             TEXT NOT NULL DEFAULT '{}',
    environment_json        TEXT NOT NULL DEFAULT '{}',
    error_message           TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_runs_scenario ON runs(scenario);
CREATE INDEX IF NOT EXISTS idx_runs_image_ref ON runs(image_ref);
CREATE INDEX IF NOT EXISTS idx_runs_image_digest ON runs(image_digest);
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
CREATE INDEX IF NOT EXISTS idx_runs_created_at ON runs(created_at);
CREATE UNIQUE INDEX IF NOT EXISTS uq_benchmark_scenario_image
    ON runs(scenario, image_ref)
    WHERE kind = 'benchmark';

CREATE TABLE IF NOT EXISTS metric_series (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id           TEXT NOT NULL,
    metric_name      TEXT NOT NULL,
    display_name     TEXT NOT NULL DEFAULT '',
    unit             TEXT NOT NULL DEFAULT '',
    prometheus_query TEXT NOT NULL DEFAULT '',
    labels_json      TEXT NOT NULL DEFAULT '{}',
    UNIQUE (run_id, metric_name, labels_json),
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_metric_series_run_id ON metric_series(run_id);

CREATE TABLE IF NOT EXISTS metric_points (
    series_id            INTEGER NOT NULL,
    timestamp            TEXT NOT NULL,
    elapsed_milliseconds INTEGER NOT NULL,
    value                REAL NOT NULL,
    FOREIGN KEY (series_id) REFERENCES metric_series(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_metric_points_series_id ON metric_points(series_id);
CREATE INDEX IF NOT EXISTS idx_metric_points_series_elapsed ON metric_points(series_id, elapsed_milliseconds);

CREATE TABLE IF NOT EXISTS validation_results (
    run_id                     TEXT PRIMARY KEY,
    passed                     INTEGER NOT NULL,
    source_count               INTEGER NOT NULL DEFAULT 0,
    expected_count             INTEGER NOT NULL DEFAULT 0,
    logical_output_count       INTEGER NOT NULL DEFAULT 0,
    physical_delivery_count    INTEGER NOT NULL DEFAULT 0,
    duplicate_delivery_count   INTEGER NOT NULL DEFAULT 0,
    duplicate_rate             REAL NOT NULL DEFAULT 0,
    missing_count              INTEGER NOT NULL DEFAULT 0,
    unexpected_count           INTEGER NOT NULL DEFAULT 0,
    corrupted_count            INTEGER NOT NULL DEFAULT 0,
    routing_mismatch_count      INTEGER NOT NULL DEFAULT 0,
    child_mismatch_count        INTEGER NOT NULL DEFAULT 0,
    detail_json                TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS run_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      TEXT NOT NULL,
    timestamp   TEXT NOT NULL,
    level       TEXT NOT NULL,
    phase       TEXT NOT NULL DEFAULT '',
    message     TEXT NOT NULL,
    detail_json TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_run_events_run_id ON run_events(run_id);
CREATE INDEX IF NOT EXISTS idx_run_events_run_ts ON run_events(run_id, timestamp);

-- Singleton harness configuration.
CREATE TABLE IF NOT EXISTS harness_config (
    id                          INTEGER PRIMARY KEY CHECK (id = 1),
    context                     TEXT NOT NULL DEFAULT '',
    cluster                     TEXT NOT NULL DEFAULT 'numaflow',
    prometheus_url              TEXT NOT NULL DEFAULT '',
    central_namespace           TEXT NOT NULL DEFAULT 'numaflow-system',
    monitoring_namespace        TEXT NOT NULL DEFAULT 'monitoring',
    validation_namespace        TEXT NOT NULL DEFAULT 'validation-system',
    log_format                  TEXT NOT NULL DEFAULT 'text' CHECK (log_format IN ('text', 'json')),
    verbose                     INTEGER NOT NULL DEFAULT 0,
    udf_image                   TEXT NOT NULL DEFAULT '',
    image                       TEXT NOT NULL DEFAULT '',
    tested_numaflow_major_minor TEXT NOT NULL DEFAULT '1.8',
    updated_at                  TEXT NOT NULL
);

-- Opaque run artifacts (manifests, diagnostics, pg dumps, etc.).
CREATE TABLE IF NOT EXISTS run_artifacts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      TEXT NOT NULL,
    kind        TEXT NOT NULL,
    name        TEXT NOT NULL,
    media_type  TEXT NOT NULL DEFAULT 'application/octet-stream',
    content     BLOB NOT NULL,
    size_bytes  INTEGER NOT NULL,
    sha256      TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    UNIQUE (run_id, kind, name),
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_run_artifacts_run_id ON run_artifacts(run_id);

-- Normalized validation failure samples (replaces failure-samples.json).
CREATE TABLE IF NOT EXISTS validation_failure_samples (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      TEXT NOT NULL,
    ordinal     INTEGER NOT NULL,
    kind        TEXT NOT NULL,
    logical_key TEXT NOT NULL DEFAULT '',
    expected    TEXT NOT NULL DEFAULT '',
    actual      TEXT NOT NULL DEFAULT '',
    detail      TEXT NOT NULL DEFAULT '',
    UNIQUE (run_id, ordinal),
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_validation_failure_samples_run_id ON validation_failure_samples(run_id);

-- Persisted comparison and single-run reports.
CREATE TABLE IF NOT EXISTS reports (
    id                  TEXT PRIMARY KEY,
    kind                TEXT NOT NULL CHECK (kind IN ('comparison', 'single')),
    scenario            TEXT NOT NULL DEFAULT '',
    generated_at        TEXT NOT NULL,
    selection_json      TEXT NOT NULL DEFAULT '{}',
    payload             BLOB NOT NULL,
    size_bytes          INTEGER NOT NULL,
    sha256              TEXT NOT NULL,
    baseline_image_ref  TEXT NOT NULL DEFAULT '',
    candidate_image_ref TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_reports_kind ON reports(kind);
CREATE INDEX IF NOT EXISTS idx_reports_scenario ON reports(scenario);
CREATE INDEX IF NOT EXISTS idx_reports_generated_at ON reports(generated_at);

CREATE TABLE IF NOT EXISTS report_runs (
    report_id TEXT NOT NULL,
    run_id    TEXT NOT NULL,
    grp       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (report_id, run_id),
    FOREIGN KEY (report_id) REFERENCES reports(id) ON DELETE CASCADE,
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_report_runs_run_id ON report_runs(run_id);

-- Database-backed exclusive locks (replaces filesystem lock files).
CREATE TABLE IF NOT EXISTS active_locks (
    lock_key     TEXT PRIMARY KEY,
    owner_token  TEXT NOT NULL,
    pid          INTEGER NOT NULL,
    host         TEXT NOT NULL DEFAULT '',
    command      TEXT NOT NULL DEFAULT '',
    run_id       TEXT NOT NULL DEFAULT '',
    namespace    TEXT NOT NULL DEFAULT '',
    scenario     TEXT NOT NULL DEFAULT '',
    image_ref    TEXT NOT NULL DEFAULT '',
    image_tag    TEXT NOT NULL DEFAULT '',
    started_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

-- Deleting a run removes reports that reference it via report_runs.
CREATE TRIGGER IF NOT EXISTS trg_runs_delete_linked_reports
BEFORE DELETE ON runs
FOR EACH ROW
BEGIN
    DELETE FROM reports
    WHERE id IN (SELECT report_id FROM report_runs WHERE run_id = OLD.id);
END;
