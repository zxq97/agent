package agent

import "testing"

func TestDecideVersionSetIncludesToolSchemaHash(t *testing.T) {
	vs, err := BuildDecideVersionSet("system prompt")
	if err != nil {
		t.Fatal(err)
	}
	if vs.PromptID == "" || vs.PromptVersion == "" || vs.ContextVersion == "" || vs.ToolSchemaHash == "" || vs.ParserVersion == "" {
		t.Fatalf("incomplete version set: %#v", vs)
	}
	again, err := BuildDecideVersionSet("system prompt")
	if err != nil {
		t.Fatal(err)
	}
	if vs.ToolSchemaHash != again.ToolSchemaHash {
		t.Fatalf("tool schema hash unstable: %s != %s", vs.ToolSchemaHash, again.ToolSchemaHash)
	}
}
