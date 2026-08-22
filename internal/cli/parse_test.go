package cli

import (
	"testing"
)

func TestSplitCLIArgsGlobalBeforeCommand(t *testing.T) {
	t.Parallel()
	p, err := splitCLIArgs([]string{"--cluster", "x", "benchmark", "list"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Command) != 1 || p.Command[0] != "benchmark" {
		t.Fatalf("command=%v", p.Command)
	}
	if len(p.GlobalArgs) != 2 || p.GlobalArgs[0] != "--cluster" {
		t.Fatalf("globals=%v", p.GlobalArgs)
	}
}

func TestSplitCLIArgsSubcommandFlagsInArgs(t *testing.T) {
	t.Parallel()
	p, err := splitCLIArgs([]string{"benchmark", "run", "--scenario", "single-map"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Command[0] != "benchmark" {
		t.Fatalf("command=%v", p.Command)
	}
	if len(p.GlobalArgs) != 0 {
		t.Fatalf("globals=%v", p.GlobalArgs)
	}
	if p.Args[0] != "run" {
		t.Fatalf("args=%v", p.Args)
	}
}

func TestSplitCLIArgsGlobalAfterCommand(t *testing.T) {
	t.Parallel()
	p, err := splitCLIArgs([]string{
		"setup", "--create-cluster", "--image", "quay.io/numaproj/numaflow:v1.8.0", "--with-validation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.GlobalArgs) != 0 {
		t.Fatalf("globals=%v", p.GlobalArgs)
	}
	if got, want := p.Args, []string{"--create-cluster", "--image", "quay.io/numaproj/numaflow:v1.8.0", "--with-validation"}; !equalStrings(got, want) {
		t.Fatalf("args=%v want %v", got, want)
	}
}

func TestSplitCLIArgsGlobalBoolAfterSubcommand(t *testing.T) {
	t.Parallel()
	p, err := splitCLIArgs([]string{
		"benchmark", "run", "--scenario", "single-map", "--verbose",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.GlobalArgs, []string{"--verbose"}; !equalStrings(got, want) {
		t.Fatalf("globals=%v want %v", got, want)
	}
	if got, want := p.Args, []string{"run", "--scenario", "single-map"}; !equalStrings(got, want) {
		t.Fatalf("args=%v want %v", got, want)
	}
}

func TestCommandPath(t *testing.T) {
	t.Parallel()
	p := parsedCLI{Command: []string{"benchmark"}, Args: []string{"run", "--scenario", "x"}}
	if commandPath(p) != "benchmark run" {
		t.Fatalf("got %q", commandPath(p))
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
