package report

import "numa-perfman/internal/runmeta"

func sourceDisplayLabel(imageRef string) string {
	tag := runmeta.DisplayImageTag(imageRef)
	if tag == "" {
		return "source unavailable"
	}
	return tag
}
