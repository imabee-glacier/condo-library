package condo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
)

// MarshalJSONL encodes records as one JSON object per line, mirroring the
// audit repo's JSONL-first style. Money remains a string in the models, so
// no floating-point rounding is introduced here. A missing field in a
// record means "not reported", not zero.
func MarshalJSONL(records []any) ([]byte, error) {
	var buf bytes.Buffer
	for _, r := range records {
		data, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// UnmarshalJSONL decodes JSONL into Bills, skipping blank lines. It is a
// convenience for state files used by check-new-bills; other record types
// should use json.Decoder directly in the same line-per-record style.
func UnmarshalBillsJSONL(r io.Reader) ([]Bill, error) {
	var out []Bill
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var b Bill
		if err := json.Unmarshal(line, &b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []Bill{}
	}
	return out, nil
}
