package logsink

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewNoDirNoStderr(t *testing.T) {
	w, c, err := New(Options{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if w != nil {
		t.Fatalf("want nil writer, got %T", w)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestNewStderrOnly(t *testing.T) {
	w, c, err := New(Options{Stderr: true})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	tw, ok := w.(*jsonLineWriter)
	if !ok || tw.inner != os.Stderr {
		t.Fatalf("want json line writer wrapping stderr, got %T", w)
	}
	_ = c.Close()
}

func TestFileWriteAndRotate(t *testing.T) {
	dir := t.TempDir()
	w, c, err := New(Options{Dir: dir, FilePrefix: "test"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer c.Close()

	if _, err := w.Write([]byte("hello\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(dir, "test-"+today+".log")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	entry := decodeLogEntry(t, strings.TrimSpace(string(got)))
	if entry["msg"] != "hello" || entry["trace_id"] != "-" || entry["tag"] != "info" {
		t.Fatalf("log entry = %#v, want json hello line", entry)
	}
	if entry["ts"] == "" {
		t.Fatalf("log entry missing ts: %#v", entry)
	}

	// 手动伪造跨天,验证 rotate
	dw := w.(*jsonLineWriter).inner.(*dailyWriter)
	dw.mu.Lock()
	dw.dateTag = "1999-01-01"
	dw.mu.Unlock()

	if _, err := w.Write([]byte("next-day\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(got2), "next-day") {
		t.Fatalf("rotated log missing next-day: %q", string(got2))
	}
}

func TestJSONLineWriterWritesEveryNonEmptyLine(t *testing.T) {
	var out strings.Builder
	tw := newJSONLineWriter(&out)
	tw.now = func() time.Time {
		return time.Date(2026, 7, 7, 9, 8, 7, 123_000_000, time.Local)
	}

	n, err := tw.Write([]byte("one\n[trace=abc123] [llm] prompt tokens=12\n\nthree"))
	if err != nil {
		t.Fatal(err)
	}
	if n != len("one\n[trace=abc123] [llm] prompt tokens=12\n\nthree") {
		t.Fatalf("n=%d", n)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines=%d out=%q", len(lines), out.String())
	}
	first := decodeLogEntry(t, lines[0])
	if first["ts"] != "2026-07-07T09:08:07.123+08:00" || first["trace_id"] != "-" || first["tag"] != "info" || first["msg"] != "one" {
		t.Fatalf("first=%#v", first)
	}
	second := decodeLogEntry(t, lines[1])
	if second["trace_id"] != "abc123" || second["tag"] != "llm" || second["msg"] != "prompt tokens=12" {
		t.Fatalf("second=%#v", second)
	}
	third := decodeLogEntry(t, lines[2])
	if third["msg"] != "three" {
		t.Fatalf("third=%#v", third)
	}
}

func decodeLogEntry(t *testing.T, line string) map[string]string {
	t.Helper()
	var entry map[string]string
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("decode log line %q: %v", line, err)
	}
	return entry
}
