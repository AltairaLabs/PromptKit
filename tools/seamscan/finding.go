package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// Finding is one candidate. seamscan never decides whether a candidate is a
// defect — that judgment belongs to the audit skill.
type Finding struct {
	Kind    string `json:"kind"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Subject string `json:"subject"`
	Detail  string `json:"detail,omitempty"`
}

// EmitText writes findings one per line, file:line first so editors can jump.
func EmitText(w io.Writer, fs []Finding) {
	for _, f := range fs {
		_, _ = fmt.Fprintf(w, "%s:%d\t%s\t%s", f.File, f.Line, f.Kind, f.Subject)
		if f.Detail != "" {
			_, _ = fmt.Fprintf(w, "\t%s", f.Detail)
		}
		_, _ = fmt.Fprintln(w)
	}
}

// EmitJSON writes findings as a JSON array, always an array even when empty so
// consumers need no special case.
func EmitJSON(w io.Writer, fs []Finding) error {
	if fs == nil {
		fs = []Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(fs)
}
