package store

import (
	"database/sql"
	"encoding/json"
	"strings"
)

// scanAll iterates SQL rows using the provided row scanner and guarantees a non-nil slice.
func scanAll[T any](rows *sql.Rows, scan func(rowScanner) (T, error)) ([]T, error) {
	out := make([]T, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// unmarshalJSONSlice decodes a JSON array string and always returns a non-nil slice.
func unmarshalJSONSlice[T any](raw string) []T {
	if strings.TrimSpace(raw) == "" {
		return []T{}
	}
	var out []T
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []T{}
	}
	return out
}

// marshalJSONSlice encodes a slice into JSON, normalizing nil or empty slices to "[]".
func marshalJSONSlice[T any](items []T) (string, error) {
	if len(items) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
