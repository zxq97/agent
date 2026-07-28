# Rental Guide Agent

## 本地浏览器页面

当前分支提供一个零依赖前端和同进程 Go HTTP/SSE 服务，可直接在浏览器中与条件化串行搜车 Pipeline 交互。车辆诉求更新、FilterPlan 编译和 Guide 搜索已经拆分，前端与 LLM 都不能提交 `filter_code` 或 `context_id`。

```bash
export DEEPSEEK_API_KEY=你的密钥
go run ./cmd/http
```

然后打开：

```text
http://localhost:8080
```

可选参数：

```bash
go run ./cmd/http \
  -config conf/dev.yaml \
  -addr :8080 \
  -web-dir web
```

页面支持：

- 本地测试用户和多会话；
- 会话新建、切换、删除及历史恢复；
- SSE 理解中、地点搜索、诉求归一、车辆搜索、结果整理及错误状态；
- 地点与时间修改、车辆诉求、搜车结果；
- Pending 候选快捷选择；
- 当前地点、取还时间、诉求和 Pending 状态展示；
- `request_id` 幂等回放和 `client_seq` 顺序检查。

会话保存在当前进程内存中，服务重启后会清空，适合本地开发联调。

当前实现使用首版静态车辆别名目录；正式环境应替换为权威、可版本化的车辆目录。Guide 多筛选项 AND/OR 和 Context 续期等未确认契约不会由客户端猜测。

## 测试

本地纯逻辑和应用测试不依赖外部服务：

```bash
go test ./...
```

Guide、Maps 和 LLM 契约测试仍读取 `conf/dev.yaml` 并调用真实服务，需要在能访问相应网络的环境显式开启：

```bash
RUN_REMOTE_INTEGRATION=1 go test ./...
```
