package cluster

import (
	"context"
	"fmt"
)

// ApplyCRDsFromPath applies only CustomResourceDefinition documents from path (file or directory).
// When strict is true, non-CRD documents cause an error instead of being skipped.
func (c Client) ApplyCRDsFromPath(ctx context.Context, path string, strict bool) error {
	docs, err := LoadYAMLDocumentsFromPath(path)
	if err != nil {
		return fmt.Errorf("load crd manifests from %q: %w", path, err)
	}
	filtered, err := FilterCRDDocuments(docs, strict)
	if err != nil {
		return err
	}
	yaml := ConcatYAMLDocuments(filtered)
	if _, err := c.ApplyYAML(ctx, "", yaml); err != nil {
		return fmt.Errorf("apply CRDs from %q: %w", path, err)
	}
	return nil
}
