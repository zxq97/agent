// Package session 提供对话历史的持久化抽象。
//
// P4 引入 Redis 实现,以支持 HTTP 服务跨请求 / 跨 pod 续聊。
// 同时保留内存实现,用于本地调试和单元测试。
//
// 注意:存的是 []*schema.Message —— 包含完整的 user / assistant(tool_calls) /
// tool 消息链路,LLM 下一轮才能从中读到真实的 reference_id 等关键字段。
package session

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// Store 是对话历史持久化抽象。所有方法都接 ctx,Redis 实现里会用到超时控制。
//
// sessionID 由调用方分配(通常等于前端给的会话标识);
// 实现里通常会再拼接 userID 做天然隔离,避免误传 sessionID 串号。
type Store interface {
	// Get 取一个会话的完整 history。会话不存在时返回 nil, nil(不视为错误)。
	Get(ctx context.Context, userID, sessionID string) ([]*schema.Message, error)

	// Save 整体覆盖一个会话的 history,并刷新 TTL。
	// 选择"整体覆盖"而非"增量追加":
	//   - history 量小(典型 < 50 条),序列化开销可忽略
	//   - 避免并发追加时 race / 半截 history
	Save(ctx context.Context, userID, sessionID string, history []*schema.Message) error

	// Touch 仅刷新 TTL 不修改内容,适合"用户访问会话摘要"场景。
	Touch(ctx context.Context, userID, sessionID string) error

	// Delete 清除一个会话。
	Delete(ctx context.Context, userID, sessionID string) error
}
