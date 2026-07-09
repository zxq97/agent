package replay

import (
	"context"
	"strings"
	"testing"
)

func TestFileStoreSaveAndFindByTraceID(t *testing.T) {
	path := t.TempDir() + "/replay.jsonl"
	store := NewFileStore(path)
	snap := LLMCallSnapshot{
		TraceID:       "trace-1",
		SessionID:     "s1",
		Stage:         "decide",
		Model:         "deepseek-chat",
		PromptVersion: "decide:v1",
		PromptHash:    "abc",
		ContextHash:   "ctx",
		ToolHash:      "tools",
		OutputText:    "ok",
	}

	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatal(err)
	}
	got, err := store.FindByTraceID(context.Background(), "trace-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Stage != "decide" || got[0].PromptHash != "abc" {
		t.Fatalf("snapshots = %#v", got)
	}
}

func TestDryReportShowsVersionAndHashDiff(t *testing.T) {
	old := LLMCallSnapshot{PromptVersion: "v1", PromptHash: "h1", ContextHash: "c1", ToolHash: "t1", Model: "m1"}
	cur := LLMCallSnapshot{PromptVersion: "v2", PromptHash: "h2", ContextHash: "c1", ToolHash: "t2", Model: "m2"}

	report := DryReport(old, cur)

	for _, want := range []string{"prompt_version: v1 -> v2", "prompt_hash: h1 -> h2", "tool_hash: t1 -> t2", "model: m1 -> m2"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}
