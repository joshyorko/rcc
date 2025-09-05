package operations

import "encoding/json"

func NiceJsonOutput(content interface{}) (string, error) {
	body, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body), nil
}
