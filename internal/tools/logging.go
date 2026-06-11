package tools

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// loggingTool 包一层 InvokableTool,在 InvokableRun 入口/出口写日志。
//
// 关键点:eino 在调 InvokableRun 时,argumentsInJSON 已经是流式累加后的**完整** JSON。
// 所以这里看到的就是 LLM 真正塞给工具的参数,远比 stream chunk 里的片段可靠。
type loggingTool struct {
	inner tool.InvokableTool
	w     io.Writer
}

func (l *loggingTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return l.inner.Info(ctx)
}

func (l *loggingTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	info, _ := l.inner.Info(ctx)
	name := "<unknown>"
	if info != nil {
		name = info.Name
	}
	start := time.Now()
	fmt.Fprintf(l.w, "[tool-in ] %s args=%s\n", name, truncate(argumentsInJSON, 800))
	out, err := l.inner.InvokableRun(ctx, argumentsInJSON, opts...)
	dur := time.Since(start)
	if err != nil {
		// Go error 理论上不应出现(tyche_wrap 已经把所有错误转 JSON 了),
		// 若真出现,说明有其他 tool 实现没做同样的处理。
		fmt.Fprintf(l.w, "[tool-err] %s in %s go-err=%v\n", name, dur, err)
		return out, err
	}
	// 识别 toolResult{is_error:true} —— 内部错误,需要在日志里显著标记。
	if isToolError(out) {
		fmt.Fprintf(l.w, "[tool-err] %s in %s result=%s\n", name, dur, truncate(out, 800))
	} else {
		fmt.Fprintf(l.w, "[tool-out] %s in %s out=%s\n", name, dur, truncate(out, 800))
	}
	return out, err
}

// isToolError 检查 tool 返回值是否为错误结构(is_error:true)。
func isToolError(s string) bool {
	return strings.Contains(s, `"is_error":true`)
}

// WrapWithLogging 把每个 InvokableTool 套上 loggingTool;非 InvokableTool 原样保留。
// 注意:返回的仍是 []tool.BaseTool,因为 eino ReAct 接收 BaseTool。
func WrapWithLogging(in []tool.BaseTool, w io.Writer) []tool.BaseTool {
	if w == nil {
		return in
	}
	out := make([]tool.BaseTool, 0, len(in))
	for _, t := range in {
		if it, ok := t.(tool.InvokableTool); ok {
			out = append(out, &loggingTool{inner: it, w: w})
			continue
		}
		out = append(out, t)
	}
	return out
}
