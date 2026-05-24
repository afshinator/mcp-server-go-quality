package diagnostic

import (
	"encoding/json"
	"testing"
)

func TestDiagnosticJSONMarshalling(t *testing.T) {
	t.Run("full diagnostic with all fields", func(t *testing.T) {
		d := Diagnostic{
			Tool:     "golangci-lint",
			File:     "cmd/main.go",
			Line:     115,
			Column:   1,
			Severity: "warning",
			Message:  "cognitive complexity 18 is high (> 15)",
			Native:   json.RawMessage(`{"FromLinter":"gocognit"}`),
		}
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		if result["tool"] != "golangci-lint" {
			t.Errorf("tool = %v", result["tool"])
		}
		if result["column"] != float64(1) {
			t.Errorf("column = %v", result["column"])
		}
		if result["native"] == nil {
			t.Error("native should not be null when populated")
		}
	})

	t.Run("zero-value fields serialize to zero", func(t *testing.T) {
		d := Diagnostic{
			Tool:    "nilaway",
			Message: "Potential nil panic detected",
		}
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		if file, ok := result["file"]; !ok || file != "" {
			t.Errorf("file should be present as \"\", got %v (%v)", file, ok)
		}
		if line, ok := result["line"]; !ok || line != float64(0) {
			t.Errorf("line should be present as 0, got %v (%v)", line, ok)
		}
		if errVal, ok := result["error"]; !ok || errVal != "" {
			t.Errorf("error should be present as \"\", got %v (%v)", errVal, ok)
		}
		if _, ok := result["column"]; ok {
			t.Error("zero column should be omitted")
		}
		if _, ok := result["severity"]; ok {
			t.Error("empty severity should be omitted")
		}
	})

	t.Run("error diagnostic has zero-valued location and message fields", func(t *testing.T) {
		d := Diagnostic{
			Tool:  "govulncheck",
			Error: "timed out after 5m0s",
		}
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		if result["error"] != "timed out after 5m0s" {
			t.Errorf("error = %v", result["error"])
		}
		if file, ok := result["file"]; !ok || file != "" {
			t.Errorf("file should be present as \"\", got %v", file)
		}
		if line, ok := result["line"]; !ok || line != float64(0) {
			t.Errorf("line should be present as 0, got %v", line)
		}
		if msg, ok := result["message"]; !ok || msg != "" {
			t.Errorf("message should be present as \"\", got %v", msg)
		}
	})

	t.Run("native is null when zero-valued", func(t *testing.T) {
		d := Diagnostic{
			Tool:  "nilaway",
			Error: "install failed: go install ...",
		}
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		nativeVal, ok := result["native"]
		if !ok {
			t.Error("native field should always be present per spec (no omitempty)")
		}
		if nativeVal != nil {
			t.Errorf("native should be null when zero-valued, got %v", nativeVal)
		}
	})
}
