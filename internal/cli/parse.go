package cli

import (
	"fmt"
	"strings"
)

// parsedCLI holds the command path and argument slices for flag parsing.
type parsedCLI struct {
	GlobalArgs []string
	Command    []string
	Args       []string
}

var topLevelCommands = map[string]struct{}{
	"setup":      {},
	"config":     {},
	"doctor":     {},
	"benchmark":  {},
	"validation": {},
	"runs":       {},
	"reports":    {},
	"serve":      {},
	"cleanup":    {},
	"help":       {},
	"version":    {},
}

func isTopLevelCommand(s string) bool {
	_, ok := topLevelCommands[s]
	return ok
}

// splitCLIArgs separates global flags from the command and its args.
// Global flags may appear before or after the command name, e.g.:
//
//	perfman --cluster numaflow benchmark run --scenario single-map
//	perfman setup --cluster numaflow --image quay.io/numaproj/numaflow:v1.8.0
func splitCLIArgs(args []string) (parsedCLI, error) {
	var out parsedCLI
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" {
			out.GlobalArgs = append(out.GlobalArgs, args[i:]...)
			return out, nil
		}
		if strings.HasPrefix(a, "-") {
			out.GlobalArgs = append(out.GlobalArgs, a)
			if needsValue(a) && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				out.GlobalArgs = append(out.GlobalArgs, args[i])
			}
			i++
			continue
		}
		if isTopLevelCommand(a) {
			out.Command = append(out.Command, a)
			i++
			for i < len(args) {
				a = args[i]
				if a == "--" {
					out.Args = append(out.Args, args[i:]...)
					break
				}
				if isGlobalFlag(a) {
					out.GlobalArgs = append(out.GlobalArgs, a)
					if globalFlagNeedsValue(a) && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
						i++
						out.GlobalArgs = append(out.GlobalArgs, args[i])
					}
				} else {
					out.Args = append(out.Args, a)
				}
				i++
			}
			return out, nil
		}
		return out, fmt.Errorf("%w: unknown command %q", errUsage, a)
	}
	return out, nil
}

var globalValueFlags = map[string]struct{}{
	"context": {}, "cluster": {}, "results-db": {},
	"prometheus-url": {}, "central-namespace": {}, "monitoring-namespace": {},
	"validation-namespace": {}, "log-format": {}, "udf-image": {},
}

var globalBoolFlags = map[string]struct{}{
	"verbose": {},
}

func flagName(arg string) string {
	name := strings.TrimLeft(arg, "-")
	if idx := strings.IndexByte(name, '='); idx >= 0 {
		name = name[:idx]
	}
	return name
}

func isGlobalFlag(arg string) bool {
	if !strings.HasPrefix(arg, "-") {
		return false
	}
	name := flagName(arg)
	_, value := globalValueFlags[name]
	_, boolean := globalBoolFlags[name]
	return value || boolean
}

func globalFlagNeedsValue(arg string) bool {
	if strings.Contains(arg, "=") {
		return false
	}
	_, ok := globalValueFlags[flagName(arg)]
	return ok
}

func needsValue(flag string) bool {
	if flag == "-h" || flag == "--help" || flag == "--version" {
		return false
	}
	if strings.HasPrefix(flag, "--") && strings.Contains(flag, "=") {
		return false
	}
	switch flag {
	case "-verbose", "--verbose", "--create-cluster", "--with-validation",
		"--retained-only", "--remove-stale-lock", "--dump-failure-db":
		return false
	}
	return strings.HasPrefix(flag, "-")
}

func commandPath(p parsedCLI) string {
	if len(p.Command) == 0 {
		return ""
	}
	parts := append([]string{}, p.Command...)
	if len(p.Args) > 0 {
		sub := p.Args[0]
		if !strings.HasPrefix(sub, "-") {
			switch p.Command[0] {
			case "benchmark", "validation", "runs", "reports", "config":
				parts = append(parts, sub)
			}
		}
	}
	return strings.Join(parts, " ")
}
