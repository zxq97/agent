package versioning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type PromptVersionSet struct {
	PromptID       string `json:"prompt_id"`
	PromptVersion  string `json:"prompt_version"`
	PromptHash     string `json:"prompt_hash"`
	ContextVersion string `json:"context_version"`
	ContextHash    string `json:"context_hash"`
	ToolSchemaSet  string `json:"tool_schema_set"`
	ToolSchemaHash string `json:"tool_schema_hash"`
	ParserVersion  string `json:"parser_version"`
}

type PromptAsset struct {
	ID      string
	Version string
	Content string
}

func (a PromptAsset) Hash() string {
	return SHA256Hex([]byte(a.ID + "\n" + a.Version + "\n" + a.Content))
}

type ToolSchemaAsset struct {
	Name    string
	Version string
	Schema  json.RawMessage
}

func (a ToolSchemaAsset) Hash() (string, error) {
	h, err := CanonicalJSONHash(a.Schema)
	if err != nil {
		return "", err
	}
	return SHA256Hex([]byte(a.Name + "\n" + a.Version + "\n" + h)), nil
}

func CanonicalJSONHash(raw []byte) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return SHA256Hex(b), nil
}

func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
