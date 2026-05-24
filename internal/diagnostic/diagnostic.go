package diagnostic

import "encoding/json"

type Diagnostic struct {
	Tool     string          `json:"tool"`
	File     string          `json:"file"`
	Line     int             `json:"line"`
	Column   int             `json:"column,omitempty"`
	Severity string          `json:"severity,omitempty"`
	Message  string          `json:"message"`
	Error    string          `json:"error"`
	Native   json.RawMessage `json:"native"`
}
