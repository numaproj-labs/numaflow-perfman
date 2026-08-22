package scenario

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"text/template"
)

const benchmarkTemplatesDir = "templates/benchmarks"

type benchmarkTemplateData struct {
	Name                string
	Namespace           string
	UDFImage            string
	ScenarioID          string
	RunID               string
	NumaflowImageDigest string
	NumaflowImageRef    string
}

var benchmarkTemplateSet *template.Template

func init() {
	var err error
	benchmarkTemplateSet, err = template.New("benchmark").Funcs(template.FuncMap{
		"yamlQuote": yamlQuote,
	}).ParseFS(benchmarkTemplates, benchmarkTemplatesDir+"/*.yaml")
	if err != nil {
		panic(fmt.Sprintf("parse benchmark templates: %v", err))
	}
}

func renderBenchmark(in RenderInput) (ManifestBundle, error) {
	scenario, err := LookupBenchmark(in.ScenarioID)
	if err != nil {
		return ManifestBundle{}, err
	}

	data := newBenchmarkTemplateData(in)
	base := benchmarkBaseName(in.ScenarioID)
	manifest, err := executeBenchmarkTemplate(base+".yaml", data)
	if err != nil {
		return ManifestBundle{}, err
	}

	if isMonoVertexBenchmark(in.ScenarioID) {
		return ManifestBundle{MonoVertex: manifest}, nil
	}

	bundle := ManifestBundle{Pipeline: manifest}
	if scenario.NeedsISB {
		bundle.ISB, err = executeBenchmarkTemplate(base+"_isb.yaml", data)
		if err != nil {
			return ManifestBundle{}, err
		}
	}
	return bundle, nil
}

func benchmarkBaseName(scenarioID string) string {
	return strings.ReplaceAll(scenarioID, "-", "_")
}

func isMonoVertexBenchmark(scenarioID string) bool {
	return strings.HasPrefix(scenarioID, "monovertex") || scenarioID == "simple-monovertex"
}

func newBenchmarkTemplateData(in RenderInput) benchmarkTemplateData {
	data := benchmarkTemplateData{
		Name:       in.ScenarioID,
		Namespace:  in.Namespace,
		UDFImage:   in.UDFImage,
		ScenarioID: in.ScenarioID,
		RunID:      in.RunID,
	}
	if in.NumaflowImage != "" {
		digest := sha256.Sum256([]byte(in.NumaflowImage))
		data.NumaflowImageDigest = fmt.Sprintf("ref-%x", digest[:8])
		data.NumaflowImageRef = yamlQuote(in.NumaflowImage)
	}
	return data
}

func executeBenchmarkTemplate(name string, data benchmarkTemplateData) (string, error) {
	var buf bytes.Buffer
	if err := benchmarkTemplateSet.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("execute benchmark template %s: %w", name, err)
	}
	return buf.String(), nil
}
