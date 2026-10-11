package environmentartifact

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

func canonicalEncode(value any) ([]byte, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode canonical JSON: %w", err)
	}
	return content, nil
}

func strictDecodeCanonical(content []byte, target any) error {
	if err := rejectDuplicateKeys(content); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode canonical JSON: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return err
	}
	canonical, err := canonicalEncode(target)
	if err != nil {
		return err
	}
	if !bytes.Equal(content, canonical) {
		return fmt.Errorf("JSON is not in canonical form")
	}
	return nil
}

func ValidateSpecificationBytes(content []byte) error {
	var specification map[string]json.RawMessage
	if err := strictDecodeCanonical(content, &specification); err != nil {
		return fmt.Errorf("invalid canonical semantic specification: %w", err)
	}
	if specification == nil {
		return fmt.Errorf("semantic specification must be a JSON object")
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return fmt.Errorf("trailing JSON value")
}

func rejectDuplicateKeys(content []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("scan JSON: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("scan JSON object key: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("scan JSON closing delimiter: %w", err)
	}
	return nil
}
