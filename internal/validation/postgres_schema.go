package validation

import "numa-perfman/internal/oracle"

// Schema DDL implements the source/sink contract documented in
// docs/validation-source-contract.md.

const mapValidationSchemaDDL = `
CREATE TABLE IF NOT EXISTS source_events (
    event_id    VARCHAR(255) PRIMARY KEY,
    user_id     VARCHAR(255) NOT NULL,
    page_id     VARCHAR(255) NOT NULL,
    ad_type     VARCHAR(50)  NOT NULL,
    event_type  VARCHAR(50)  NOT NULL,
    event_time  TIMESTAMP    NOT NULL,
    ip_address  VARCHAR(45)  NOT NULL,
    route_tag   VARCHAR(50)  NOT NULL,
    ack_status  VARCHAR(20)  NOT NULL DEFAULT 'PENDING',
    created_at  TIMESTAMP    DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_source_ack_status_nacked ON source_events(ack_status, created_at) WHERE ack_status = 'NACKED';
CREATE INDEX IF NOT EXISTS idx_source_ack_status_pending ON source_events(ack_status, created_at) WHERE ack_status = 'PENDING';

CREATE TABLE IF NOT EXISTS sink_events (
    dedup_key       VARCHAR(512) PRIMARY KEY,
    event_id        VARCHAR(255) NOT NULL,
    payload         JSONB        NOT NULL,
    processed_by    VARCHAR(50)  NOT NULL,
    child_index     VARCHAR(16)  NOT NULL,
    total_children  VARCHAR(16)  NOT NULL,
    receive_count   INT          DEFAULT 1,
    created_at      TIMESTAMP    DEFAULT NOW(),
    updated_at      TIMESTAMP    DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sink_event_id ON sink_events(event_id);

CREATE TABLE IF NOT EXISTS expected_sink_events (
    dedup_key       VARCHAR(512) PRIMARY KEY,
    event_id        VARCHAR(255) NOT NULL,
    payload         JSONB        NOT NULL,
    processed_by    VARCHAR(50)  NOT NULL,
    child_index     VARCHAR(16)  NOT NULL,
    total_children  VARCHAR(16)  NOT NULL,
    created_at      TIMESTAMP    DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_expected_event_id ON expected_sink_events(event_id);

CREATE TABLE IF NOT EXISTS run_config (
    total_events      BIGINT NOT NULL DEFAULT 0,
    source_completed  BOOLEAN DEFAULT FALSE
);
INSERT INTO run_config (total_events, source_completed) VALUES (0, FALSE);
`

const reduceValidationSchemaDDL = `
CREATE TABLE IF NOT EXISTS source_events (
    event_id    VARCHAR(255) PRIMARY KEY,
    reduce_key  VARCHAR(100) NOT NULL,
    category    VARCHAR(50)  NOT NULL,
    amount      INT          NOT NULL,
    event_time  BIGINT       NOT NULL,
    ack_status  VARCHAR(20)  NOT NULL DEFAULT 'PENDING',
    created_at  TIMESTAMP    DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_source_ack_status_nacked
  ON source_events(ack_status, created_at) WHERE ack_status = 'NACKED';
CREATE INDEX IF NOT EXISTS idx_source_ack_status_pending
  ON source_events(ack_status, created_at) WHERE ack_status = 'PENDING';

CREATE TABLE IF NOT EXISTS sink_events (
    dedup_key       VARCHAR(512) PRIMARY KEY,
    event_id        VARCHAR(255) NOT NULL,
    payload         JSONB        NOT NULL,
    processed_by    VARCHAR(50)  NOT NULL,
    child_index     VARCHAR(16)  NOT NULL,
    total_children  VARCHAR(16)  NOT NULL,
    receive_count   INT          DEFAULT 1,
    created_at      TIMESTAMP    DEFAULT NOW(),
    updated_at      TIMESTAMP    DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sink_event_id ON sink_events(event_id);

CREATE TABLE IF NOT EXISTS expected_sink_events (
    dedup_key       VARCHAR(512) PRIMARY KEY,
    event_id        VARCHAR(255) NOT NULL,
    payload         JSONB        NOT NULL,
    processed_by    VARCHAR(50)  NOT NULL,
    child_index     VARCHAR(16)  NOT NULL,
    total_children  VARCHAR(16)  NOT NULL,
    created_at      TIMESTAMP    DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_expected_event_id ON expected_sink_events(event_id);

CREATE TABLE IF NOT EXISTS run_config (
    total_events      BIGINT NOT NULL DEFAULT 0,
    source_completed  BOOLEAN DEFAULT FALSE
);
INSERT INTO run_config (total_events, source_completed) VALUES (0, FALSE);
`

func schemaDDLForScenario(s oracle.Scenario) string {
	switch s {
	case oracle.ScenarioReduce, oracle.ScenarioSlidingReduce:
		return reduceValidationSchemaDDL
	default:
		return mapValidationSchemaDDL
	}
}
