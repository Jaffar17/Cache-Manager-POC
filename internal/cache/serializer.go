package cache

import "encoding/json"

// Serializer defines marshaling boundaries for cache payloads.
type Serializer interface {
	Marshal(value any) ([]byte, error)
	Unmarshal(data []byte, dest any) error
}

// JSONSerializer implements Serializer using encoding/json.
type JSONSerializer struct{}

func (JSONSerializer) Marshal(value any) ([]byte, error) {
	return json.Marshal(value)
}

func (JSONSerializer) Unmarshal(data []byte, dest any) error {
	return json.Unmarshal(data, dest)
}
