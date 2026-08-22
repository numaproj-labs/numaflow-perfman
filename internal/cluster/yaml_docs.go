package cluster

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const crdKind = "CustomResourceDefinition"

// SplitYAMLDocuments splits multi-document YAML on --- separators.
func SplitYAMLDocuments(data []byte) [][]byte {
	raw := string(data)
	raw = strings.TrimPrefix(raw, "\ufeff")
	parts := strings.Split(raw, "\n---")
	var docs [][]byte
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i > 0 {
			part = strings.TrimPrefix(part, "\n")
		}
		docs = append(docs, []byte(part))
	}
	return docs
}

// DocumentKind returns the Kubernetes kind from a YAML document (first kind: line).
func DocumentKind(doc []byte) string {
	for _, line := range strings.Split(string(doc), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "kind:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "kind:"))
		}
	}
	return ""
}

// FilterCRDDocuments keeps only CustomResourceDefinition documents.
// When strict is true, returns an error if any document is not a CRD.
func FilterCRDDocuments(docs [][]byte, strict bool) ([][]byte, error) {
	var out [][]byte
	for _, doc := range docs {
		kind := DocumentKind(doc)
		if kind == "" {
			if strict {
				return nil, fmt.Errorf("yaml document missing kind")
			}
			continue
		}
		if kind != crdKind {
			if strict {
				return nil, fmt.Errorf("reject non-CRD document kind %q (--crds must be CRD-only)", kind)
			}
			continue
		}
		out = append(out, doc)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no CustomResourceDefinition documents found")
	}
	return out, nil
}

// ConcatYAMLDocuments joins documents with --- separators for kubectl apply.
func ConcatYAMLDocuments(docs [][]byte) []byte {
	var b bytes.Buffer
	for i, doc := range docs {
		if i > 0 {
			b.WriteString("---\n")
		}
		b.Write(doc)
		if len(doc) > 0 && doc[len(doc)-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	return b.Bytes()
}

// LoadYAMLDocumentsFromPath reads a file or all .yaml/.yml files in a directory.
func LoadYAMLDocumentsFromPath(path string) ([][]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return SplitYAMLDocuments(data), nil
	}
	var docs [][]byte
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		docs = append(docs, SplitYAMLDocuments(data)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("no yaml documents under %q", path)
	}
	return docs, nil
}
