package federation

import "encoding/json"

// jsonUnmarshalLax tolerates empty/null payloads.
func jsonUnmarshalLax(data []byte, v any) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	return json.Unmarshal(data, v)
}
