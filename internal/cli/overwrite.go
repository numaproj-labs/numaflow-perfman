package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"numa-perfman/internal/results"
)

type benchmarkOverwriteConfirmer struct {
	input  io.Reader
	output io.Writer
	yes    bool
}

func (c benchmarkOverwriteConfirmer) ConfirmOverwrite(_ context.Context, existing results.Run) (bool, error) {
	if c.yes {
		return true, nil
	}
	if c.input == nil || c.output == nil {
		return false, nil
	}
	fmt.Fprintf(c.output,
		"benchmark result exists: run=%s scenario=%s image=%s status=%s created=%s\nOverwrite it? [y/N]: ",
		existing.ID, existing.Scenario, existing.ImageRef, existing.Status, existing.CreatedAt.Format(time.RFC3339),
	)
	answer, err := bufio.NewReader(c.input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read overwrite confirmation: %w", err)
	}
	answer = strings.TrimSpace(answer)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}
