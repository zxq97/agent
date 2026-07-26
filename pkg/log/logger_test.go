package log

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteCarriesTraceID(t *testing.T) {
	var output bytes.Buffer
	Init(NewJSONLogger(&output))
	t.Cleanup(func() { Init(nil) })
	ctx := WithTraceID(context.Background(), "trace-123")
	Write(ctx, Entry{Component: "http", Operation: "maps search", Request: "request", Response: "response", DurationMS: 12})

	var entry Entry
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.TraceID != "trace-123" || entry.Operation != "maps search" || entry.DurationMS != 12 {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestWriteOmitsTraceIDWhenContextHasNone(t *testing.T) {
	var output bytes.Buffer
	Init(NewJSONLogger(&output))
	t.Cleanup(func() { Init(nil) })
	Write(context.Background(), Entry{Component: "http", Operation: "maps search"})

	var raw map[string]any
	if err := json.Unmarshal(output.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["trace_id"]; ok {
		t.Fatalf("trace_id should be omitted: %s", output.String())
	}
}

func TestWriteUsesContextLogger(t *testing.T) {
	var defaultOutput bytes.Buffer
	var requestOutput bytes.Buffer
	Init(NewJSONLogger(&defaultOutput))
	t.Cleanup(func() { Init(nil) })
	ctx := WithLogger(context.Background(), NewJSONLogger(&requestOutput))
	ctx = WithTraceID(ctx, "trace-request")
	Write(ctx, Entry{Component: "http", Operation: "chat"})
	if defaultOutput.Len() != 0 {
		t.Fatalf("default logger received request entry: %s", defaultOutput.String())
	}
	if !bytes.Contains(requestOutput.Bytes(), []byte(`"trace_id":"trace-request"`)) {
		t.Fatalf("request log = %s", requestOutput.String())
	}
}

func TestNewDailyFileLogger(t *testing.T) {
	logger, closer, err := NewDailyFileLogger(t.TempDir(), "agent")
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	Init(logger)
	t.Cleanup(func() { Init(nil) })
	ctx := WithTraceID(context.Background(), "trace-file")
	Write(ctx, Entry{Component: "test", Operation: "file"})
	path := filepath.Join(filepath.Dir(closer.(*os.File).Name()), "agent-"+time.Now().Format("2006-01-02")+".log")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte(`"trace_id":"trace-file"`)) {
		t.Fatalf("content = %s", content)
	}
}
