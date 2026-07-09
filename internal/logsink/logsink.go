// Package logsink 提供带按天切分的文件日志 writer。
//
// 用法:
//
//	w, closer, err := logsink.New(logsink.Options{Dir: ".logs", Stderr: true})
//	if err != nil { ... }
//	defer closer.Close()
//	// 把 w 传给 factory.SetLogger / tools.NewDeps / agent.New / httphandler.New
//
// 写入策略:每次 Write 检查当前日期,跨天则原子替换底层文件,不阻塞其他 goroutine。
// 文件名格式:<dir>/agent-YYYY-MM-DD.log
package logsink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Options logsink 构造参数。
type Options struct {
	// Dir 日志目录,不存在则创建。空字符串禁用文件输出。
	Dir string
	// Stderr 是否同时把日志复制一份到 os.Stderr。
	Stderr bool
	// FilePrefix 文件名前缀,默认 "agent"。产物为 <dir>/<prefix>-YYYY-MM-DD.log。
	FilePrefix string
}

// New 按 opts 构造 writer + closer。
//
//   - Dir 为空且 Stderr=false → 返回 (nil, nopCloser, nil),等价于关闭日志。
//   - Dir 为空且 Stderr=true  → 只返回 stderr。
//   - Dir 非空                → 打开文件,按天切分;Stderr=true 时 io.MultiWriter 兼输出。
func New(opts Options) (io.Writer, io.Closer, error) {
	if opts.FilePrefix == "" {
		opts.FilePrefix = "agent"
	}

	if opts.Dir == "" {
		if opts.Stderr {
			return newJSONLineWriter(os.Stderr), nopCloser{}, nil
		}
		return nil, nopCloser{}, nil
	}

	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("mkdir log dir %s: %w", opts.Dir, err)
	}

	dw := &dailyWriter{dir: opts.Dir, prefix: opts.FilePrefix}
	if err := dw.rotate(time.Now()); err != nil {
		return nil, nil, err
	}

	if opts.Stderr {
		return newJSONLineWriter(io.MultiWriter(os.Stderr, dw)), dw, nil
	}
	return newJSONLineWriter(dw), dw, nil
}

type jsonLineWriter struct {
	mu    sync.Mutex
	inner io.Writer
	now   func() time.Time
}

func newJSONLineWriter(inner io.Writer) *jsonLineWriter {
	return &jsonLineWriter{
		inner: inner,
		now:   time.Now,
	}
}

func (w *jsonLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	var b bytes.Buffer
	ts := w.now().Format("2006-01-02T15:04:05.000-07:00")
	for _, line := range strings.Split(string(p), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry := parseLogLine(line)
		entry.TS = ts
		data, err := json.Marshal(entry)
		if err != nil {
			return 0, err
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		return len(p), nil
	}
	if _, err := w.inner.Write(b.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}

type logEntry struct {
	TS      string `json:"ts"`
	Level   string `json:"level"`
	TraceID string `json:"trace_id"`
	Tag     string `json:"tag"`
	Msg     string `json:"msg"`
}

func parseLogLine(line string) logEntry {
	entry := logEntry{
		Level:   "info",
		TraceID: "-",
		Tag:     "info",
		Msg:     line,
	}
	if strings.HasPrefix(line, "[trace=") {
		if end := strings.Index(line, "]"); end > len("[trace=") {
			entry.TraceID = line[len("[trace="):end]
			line = strings.TrimSpace(line[end+1:])
			entry.Msg = line
		}
	}
	if strings.HasPrefix(line, "[") {
		if end := strings.Index(line, "]"); end > 1 {
			entry.Tag = line[1:end]
			entry.Msg = strings.TrimSpace(line[end+1:])
		}
	}
	return entry
}

// dailyWriter 按天切分的文件 writer。线程安全。
type dailyWriter struct {
	dir    string
	prefix string

	mu      sync.Mutex
	file    *os.File
	dateTag string // 当前打开文件的 "YYYY-MM-DD"
}

// Write 实现 io.Writer。跨天时自动 rotate;rotate 失败不吞消息,尝试写旧句柄。
func (d *dailyWriter) Write(p []byte) (int, error) {
	now := time.Now()
	tag := now.Format("2006-01-02")

	d.mu.Lock()
	if tag != d.dateTag {
		// rotate 失败继续用旧句柄写,保证消息不丢
		_ = d.rotate(now)
	}
	f := d.file
	d.mu.Unlock()

	if f == nil {
		return len(p), nil
	}
	return f.Write(p)
}

// rotate 必须在 d.mu 保护下调用。打开新文件并关闭旧文件。
func (d *dailyWriter) rotate(now time.Time) error {
	tag := now.Format("2006-01-02")
	name := filepath.Join(d.dir, fmt.Sprintf("%s-%s.log", d.prefix, tag))
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", name, err)
	}
	if d.file != nil {
		_ = d.file.Close()
	}
	d.file = f
	d.dateTag = tag
	return nil
}

// Close 关闭底层文件。
func (d *dailyWriter) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file == nil {
		return nil
	}
	err := d.file.Close()
	d.file = nil
	return err
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
