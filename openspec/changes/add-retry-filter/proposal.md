## Why

池模式（`Account.IsPoolMode()`，上游指向另一个 sub2api 实例或账号池）下，「这次错误要不要在同一账号上重试」
目前**只看 HTTP 状态码**：账号级可配 `pool_mode_retry_status_codes`（未配置时回落 `[401, 403, 429]`）
与 `pool_mode_retry_count`（默认 3、上限 10），判定逻辑在 `Account.IsPoolModeRetryableStatus(statusCode)`
（`backend/internal/service/account.go:1222`），被 16 个文件的 26 处调用点直接内联使用
（形如 `RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode)`）。
这 26 处是**现状描述，不是本变更的改动面**——它们一行不改，理由见 `## Impact`。

同一个状态码背后的语义差别很大。以 429 为例，上游正文写 `rate limit, retry later` 时重试是对的，
写 `quota exhausted` 时重试纯属浪费——既拖长客户端等待，又白耗上游额度与本地并发位。
管理员当前**没有任何无代码手段**表达「这一类错误别重试」，只能整档打开或整档关闭某个状态码。

同时，项目里已经存在一套形状几乎相同、且已在生产运行的规则子系统——**错误透传规则**
（`model.ErrorPassthroughRule` + 仓储 + 缓存 + 管理端 handler + `ErrorPassthroughRulesModal.vue`），
它已经支持「错误码列表 OR 关键词列表 + any/all 匹配模式 + 优先级 + 平台范围」。
本变更不重新发明规则引擎，而是**沿用这套已验证的形状**，换成重试维度的判定与动作。

## What Changes

- 新增**默认关闭**的总开关 `retry_filter_enabled`（设置默认值写在代码里，`false`）。
  开关关闭时，判定结果**与现状逐字节等价**，不产生任何行为变化。
- 新增**独立管理页**「重试过滤」，开关关闭时该页不可进入（沿用风控页的路由 meta 门控形状）。
  左侧导航使用与“安全审计”一致的折叠父菜单，分为“过滤规则管理”和“重试监控”。
  页内以“预设即规则集”的方式无代码编排：预设信息、规则编辑与拖拽排序内嵌；账号/分组搜索、多选绑定和全局默认也在同一层完成。
- **判定维度**（六类配置，命中条件之间可选 `any` / `all`）：
  - HTTP 状态码列表（现有能力，迁移到规则里统一管）
  - 上游响应正文关键词列表（子串匹配，大小写不敏感，只看前 8KB）
  - 上游自带错误码字段（`error.type` / `error.code` 等 JSON 路径取值后精确匹配）
  - 请求的模型名（支持 `*` 后缀通配，如 `claude-opus-*`）
  - 空请求：区分空请求体与结构化内容为空
  - 空响应：区分正常结束但无有效语义输出，与建立流后无有效输出即中断
- **命中动作**（四种，互斥单选）：
  - `same_account_retry`——在本账号重试（**默认动作**，等于现有行为），可把重试次数覆盖为 0–50，并把每次重试间隔配置为 0–60000ms
  - `switch_account`——不在本账号重试，直接换账号，**且不因此惩罚该账号**
  - `terminate`——不重试，直接把错误返回给客户端
  - `observe`——只记录命中、不改变任何行为（供正式启用前空跑验证）
- **预设（模板）**：规则归属于具名预设，**可建多套**；预设可被**全局 / 分组 / 账号**三级引用，
  优先级 **账号 > 分组 > 全局**，取到第一个即用，不做跨级合并。
  账号级引用即「对单独账户使用预设管理」，落在 `Account.Credentials["retry_filter_preset_id"]`，零迁移。
- **重试监控**：复用既有 `ops_error_logs.upstream_errors` JSON，展示命中时的预设、规则、动作、
  空事件类型、行为是否改变、重试次数与间隔快照；不新增审计表或数据库列。
- **作用范围闸门**：v1 只对 `IsPoolMode()` 为真的账号生效；范围本身做成配置项（`scope`），
  代码不假设「只有池模式」，以后放开只需增加一个范围取值，不需重构。
- **页面须标注两条已知限制**（不标注就是让管理员误判生效范围）：
  - OpenAI WebSocket 链路保留既有前置条件（`StatusCode == 429` 且 `SameAccountRetryDeadline` 非零），
    过滤器**不能**在该链路上把非 429 错误升级为同账号重试。
  - `openai_alpha_search.go` 与 `openai_images.go` 有两处**不经门控**的账号失败上报，
    不在本变更修改范围，因此这两条链路选 `switch_account` 时**仍会喂一次失败给熔断器**。

## Capabilities

### New Capabilities
- `retry-filter-rules`：重试过滤规则与预设的数据模型、校验、仓储与缓存；管理端 CRUD 与独立管理页。
- `retry-filter-runtime`：网关链路上的判定与动作落地；默认关闭时的等价性保证；观察模式记录。

### Modified Capabilities
- 池模式同账号重试判定：由 `Account.IsPoolModeRetryableStatus(statusCode)` 的**状态码单一维度**，
  扩展为「规则命中 → 动作」。账号上现有的 `pool_mode_retry_status_codes`／`pool_mode_retry_count`
  **保留且继续生效**，作为未命中任何规则时的回落，不做数据迁移、不做字段废弃。
- 账号惩罚语义：`UpstreamFailoverError` 新增 `SuppressAccountPenalty`，使「换号」与「罚账号」
  从现状的绑定关系中解开。既有字段语义全部不变。

## Impact

- **数据库**：新增 2 张表（预设、规则），迁移编号 **239、240**（当前最大为 238；官方自身存在
  两个 238 号迁移 `238_opencode_go_platform.sql` 与 `238_purge_unlimited_user_platform_quotas.sql`，
  均为官方提交，不属本项目问题）。分组新增一个可空的 JSONB 配置列；账号级引用走 `Credentials`，
  **不需要迁移**。**不改动任何既有列语义。**
- **后端**：新增 `model` / `repository`（含缓存）/ `service` / `handler/admin` 四层。
  **那 26 处判定点一行不改**——判定收敛到同账号重试的**一对共享包级函数**
  `sameAccountRetryAllowed`（`backend/internal/handler/failover_loop.go:79`）与
  `effectiveSameAccountRetryLimit`（同文件 `:113`）。二者的非测试调用点**共 8 个、全部在
  `package handler` 内**，覆盖**两族全部链路**：族 A 的 `HandleFailoverError`（一处改动覆盖 5 个入口）
  与族 B 的 7 个内联循环（OpenAI Responses／Anthropic 兼容／WebSocket／Chat Completions／
  Images／Alpha Search、Grok Media）。理由与逐点实测见 `design.md`「裁决点实测」。
  改动面合计五项：① 上述一对函数的签名；② 8 个调用点的实参；
  ③ 族 A `HandleFailoverError` 改为收入参结构体，涉及 5 个调用方；
  ④ **净增**：`openai_alpha_search.go` 与 `openai_images.go` 的循环**从不检查
  `ShouldRetryNextAccount()`**（其余 6 处都有），各需补一次，否则 `terminate` 在这两条链路上
  **静默失效**——页面上配了直接返回、实际仍在换号；
  ⑤ 对 `UpstreamFailoverError` 新增 1 个字段 `SuppressAccountPenalty`（不复用
  `RequestScopedTransient`——后者注释明写「可同账号重试」，与 `switch_account` 语义相反），
  落点两处各一行早退：`TempUnscheduleRetryableError`（`backend/internal/service/gateway_service.go:743`，
  族 A 的临时封禁）与 `ShouldReportAccountScheduleFailure`（同文件 `:723`，族 B 的熔断器门控）。
- **连带影响**：族 B 那条早退会同时跳过 `scheduler.ReportResult(..., false, ...)`，
  即该次失败**也不计入调度器负载均衡健康度**。语义上与「这个错不是账号的锅」一致，
  但它确实改变了调度器观测到的数据，不是纯粹的「少罚一次」。受作用范围闸门约束，
  该影响只出现在池模式账号上。若日后发现调度器选号偏斜，回看这一条。
- **管理端 API**：新增预设与规则的 CRUD；分组、账号的 DTO 各新增一个预设引用字段。
- **前端**：新增独立页与路由门控、设置页开关、中英文案（本项目 i18n 只有 `zh` 与 `en`）。
- **上游影响**：无新增上游请求。判定所需的状态码、正文、响应头在构造 `UpstreamFailoverError`
  时**已经读好**（`readUpstreamErrorBody`），不需要额外读流，不影响流式转发。
- **同步冲突面**：`openai_gateway_handler.go` 是官方高频改动文件，v1 在其中占 3 个挂钩点，后续同步可能产生冲突。每个挂钩点的改动限定为判定函数实参调整，以缩小冲突面。
- **权限边界**：管理员功能，全部挂 `/api/v1/admin/*`；普通用户不可见、不可配。

## Non-goals

- 不做正则匹配（v1 只做子串与 `*` 后缀通配）。理由写在 `design.md` 的开放问题 (c)。
- 不重排两族的循环控制流：不抽公共循环、不动周边分支、不改切号与耗尽处置的既有顺序。
  但本变更**确实不是零结构改动**——新增 1 个 `UpstreamFailoverError` 字段，
  并给 `openai_alpha_search.go`／`openai_images.go` 各补一次 `ShouldRetryNextAccount()` 检查
  （该检查本就是 `terminate` 的既有生效条件，其余 6 处已有，此处是补齐而非新机制）。
- **不放开账号作用范围**。这里有一个必须分清的区别：
  - **挂钩点覆盖**：v1 接入 OpenAI 族，指的是 `package handler` 内 8 个判定调用点全部接上。
  - **账号作用范围**：v1 的范围闸门只对 `IsPoolMode()` 为真的账号返回非空判定，
    而 `IsPoolMode()` 要求 `IsAPIKeyOrBedrock()`（`account.go:1092`、`:1292`），
    即只有 **API Key 与 Bedrock** 两类账号成立。
  - 族 B 的循环同时服务 OAuth 账号，但闸门会让过滤器对这些账号返回空判定、走现状逻辑。
    所以**接入 OpenAI 族不等于放开 OAuth 账号**，OAuth 账号的重试行为 v1 不变。
- 不废弃、不迁移账号上现有的两个池模式旋钮。
- 不新增审计表：`observe` 的留痕走结构化日志与既有请求级 ops 事件 JSON。
- 不修改那两处无门控的账号失败上报（`openai_alpha_search.go:207`、`openai_images.go:294`），
  由此产生的不完整在页面上写明，不在代码里掩盖。
- 不修正官方自身的 238 号迁移重号。
