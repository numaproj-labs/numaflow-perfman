package results

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const harnessConfigSingletonID = 1

// LoadHarnessConfig returns the singleton harness configuration row, if present.
func (r *Repository) LoadHarnessConfig(ctx context.Context) (HarnessConfig, bool, error) {
	var cfg HarnessConfig
	var verbose int
	var updatedAt string
	err := r.db.QueryRowContext(ctx, `
		SELECT context, cluster, prometheus_url, central_namespace, monitoring_namespace,
			validation_namespace, log_format, verbose, udf_image, image,
			tested_numaflow_major_minor, updated_at
		FROM harness_config WHERE id = ?`, harnessConfigSingletonID,
	).Scan(
		&cfg.Context, &cfg.Cluster, &cfg.PrometheusURL, &cfg.CentralNamespace, &cfg.MonitoringNamespace,
		&cfg.ValidationNamespace, &cfg.LogFormat, &verbose, &cfg.UDFImage, &cfg.Image,
		&cfg.TestedNumaflowMajorMinor, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return HarnessConfig{}, false, nil
	}
	if err != nil {
		return HarnessConfig{}, false, fmt.Errorf("load harness config: %w", err)
	}
	cfg.Verbose = verbose != 0
	t, err := parseTime(updatedAt)
	if err != nil {
		return HarnessConfig{}, false, fmt.Errorf("parse harness config updated_at: %w", err)
	}
	cfg.UpdatedAt = t
	return cfg, true, nil
}

// UpsertHarnessConfig inserts or replaces the singleton harness configuration row.
func (r *Repository) UpsertHarnessConfig(ctx context.Context, cfg HarnessConfig) error {
	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		return fmt.Errorf("invalid log_format %q", cfg.LogFormat)
	}
	updatedAt := cfg.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	verbose := 0
	if cfg.Verbose {
		verbose = 1
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO harness_config (
			id, context, cluster, prometheus_url, central_namespace, monitoring_namespace,
			validation_namespace, log_format, verbose, udf_image, image,
			tested_numaflow_major_minor, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			context = excluded.context,
			cluster = excluded.cluster,
			prometheus_url = excluded.prometheus_url,
			central_namespace = excluded.central_namespace,
			monitoring_namespace = excluded.monitoring_namespace,
			validation_namespace = excluded.validation_namespace,
			log_format = excluded.log_format,
			verbose = excluded.verbose,
			udf_image = excluded.udf_image,
			image = excluded.image,
			tested_numaflow_major_minor = excluded.tested_numaflow_major_minor,
			updated_at = excluded.updated_at`,
		harnessConfigSingletonID, cfg.Context, cfg.Cluster, cfg.PrometheusURL, cfg.CentralNamespace,
		cfg.MonitoringNamespace, cfg.ValidationNamespace, cfg.LogFormat, verbose, cfg.UDFImage,
		cfg.Image, cfg.TestedNumaflowMajorMinor, formatTime(updatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert harness config: %w", err)
	}
	return nil
}
