# sticky-session key-level simplification

## Goal

将 sticky/session 路由语义收敛为“按 API key 粘性绑定到一个渠道”。同一个 key 下的请求不再按 response chain、prefix、prompt cache 或 request shape 拆分，成功后只保留 key -> channel 的稳定绑定。目标是删掉多余的兼容语义和分叉逻辑，让路由入口、fallback、回写规则都更简单、更可预测。

## What I already know

* 旧实现曾经同时使用 session id、previous_response_id、prompt cache、transcript prefix 等作为 lookup。
* 旧实现也会把 response id、previous response id、completed prefix 等别名一并刷新。
* 旧实现的绑定命中还受 route tier 限制，绑定目标必须落在当前有效 tier。
* 用户明确要求破坏性更新，不接受补丁式修修补补。
* 用户的目标是：一个 key 粘到一个渠道上，不区分 key 内部的不同请求。

## Assumptions (temporary)

* sticky 身份主键收敛为 APIKeyID，必要时叠加 ProjectID 作为命名空间，但不再使用 request fingerprint 作为主身份。
* 只保留“成功后回写 key -> channel”的单一绑定路径。
* 路由 tier 仍保留用于冷启动和候选排序，但不再压制已存在的 key-level sticky 绑定。

## Decisions

* 不保留 legacy sticky 键的读取兼容，本次直接切断。
* 绑定成功后保留 credential fingerprint 作为调试信息，但不把它当作 sticky 路由身份。

## Requirements (evolving)

* 删除 response chain / prefix / prompt cache 作为 sticky 身份来源。
* 删除 sticky 回写中的多别名刷新逻辑，改成 key-level 单点回写。
* 删除或简化 route tier 对已有 sticky 绑定的拦截。
* 保留失败显式暴露，不做 silent fallback。
* 保留必要的调试日志，但日志不得重新引入多身份路由语义。

## Acceptance Criteria (evolving)

* [ ] 同一个 API key 的后续请求会优先落到上次成功的渠道。
* [ ] 不同 response chain 不再把同一个 key 拆成多个 sticky 身份。
* [ ] sticky 回写只更新单一 key-level 绑定。
* [ ] route tier 不再阻止已有 key-level sticky 绑定命中。
* [ ] 相关单元测试覆盖 key-level 粘性、失败 fallback、成功重绑和旧语义删除。

## Definition of Done (team quality bar)

* Tests added/updated (unit/integration where appropriate)
* Lint / typecheck / CI green
* Docs/notes updated if behavior changes
* Rollout/rollback considered if risky

## Out of Scope (explicit)

* 不做“补丁式”兼容修复。
* 不保留 response chain / prefix / prompt cache 的 sticky 路由身份。
* 不引入新的 silent fallback。
* 不改成更复杂的多层会话路由。

## Technical Notes

* 重点文件：`internal/server/orchestrator/sticky_session.go`, `internal/server/orchestrator/outbound.go`, `internal/server/orchestrator/select_candidates.go`, `internal/server/orchestrator/state.go`, `internal/server/biz/channel_apikey_provider.go`。
* 已确认旧实现会把 `previous_response_id`、prefix、prompt cache 等纳入 sticky 身份。
* 已确认旧实现对绑定命中仍有 route tier 限制。
