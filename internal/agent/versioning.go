package agent

import (
	"encoding/json"

	"github.com/zxq97/rental-agent/internal/versioning"
)

func BuildDecideVersionSet(systemPrompt string) (versioning.PromptVersionSet, error) {
	toolHash, err := decideToolSchemaHash()
	if err != nil {
		return versioning.PromptVersionSet{}, err
	}
	return versioning.PromptVersionSet{
		PromptID:       versioning.PromptDecideID,
		PromptVersion:  versioning.PromptDecideVersion,
		PromptHash:     versioning.PromptAsset{ID: versioning.PromptDecideID, Version: versioning.PromptDecideVersion, Content: systemPrompt}.Hash(),
		ContextVersion: versioning.ContextStatePrefixVersion,
		ContextHash:    versioning.SHA256Hex([]byte(versioning.ContextStatePrefixVersion)),
		ToolSchemaSet:  versioning.ToolSchemaSetDecide,
		ToolSchemaHash: toolHash,
		ParserVersion:  versioning.ParserVersionDecide,
	}, nil
}

func decideToolSchemaHash() (string, error) {
	b, err := json.Marshal(decideTools())
	if err != nil {
		return "", err
	}
	return versioning.CanonicalJSONHash(b)
}
