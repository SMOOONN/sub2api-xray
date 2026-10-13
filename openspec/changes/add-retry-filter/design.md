## 背景与现状实测

本节代码位置与计数记录的是 v0.2.5-xray5 设计基线，用于说明本变更的接入位置。本文不作为当前版本编译、测试或部署通过的证据。

### 现状：池模式重试只看状态码

```text
Account.IsPoolMode()                      backend/internal/service/account.go:1092
Account.GetPoolModeRetryCount()           backend/internal/service/account.go:1111   默认 3，上限 10
Account.GetPoolModeRetryStatusCodes()     backend/internal/service/account.go:1169   nil=未配置，[]=显式关闭
Account.IsPoolModeRetryableStatus(code)   backend/internal/service/account.go:1222
defaultPoolModeRetryableStatusCodes       backend/internal/service/account.go:1151   [401, 403, 429]
```

`IsPoolModeRetryableStatus` 在**非测试代码中被调用 26 次，分布在 16 个文件**，几乎全部是
构造 `UpstreamFailoverError` 时内联一行：

```go
RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode),
```

### 决策所需数据在构造点已经齐备

`UpstreamFailoverError`（`backend/internal/service/gateway_service.go:686`）已经携带：

| 字段 | 判定用途 |
|---|---|
| `StatusCode int` | 维度①状态码 |
| `ResponseBody []byte` | 维度②正文关键词、维度③错误码字段（注释原文即「用于错误透传规则匹配」） |
| `ResponseHeaders http.Header` | 备用 |

`ResponseBody` 由 `readUpstreamErrorBody(resp)` 在构造前读好，随后 `resp.Body` 被替换为
`io.NopCloser(bytes.NewReader(respBody))` 复位。**67 个构造点中 59 个带 `ResponseBody`。**
所以判定**不需要额外读流，不影响流式转发**。

### 关键实测结论：判定点必须收到循环层，不能铺在 26 个构造点上

我逐个检查了 26 个构造点所在函数的作用域，得到两条硬约束：

**(1) 模型名在 26 个点里只有 11 个能看到。**

| 能看到模型名 | 变量名 |
|---|---|
| `gateway_forward.go:733` | `reqModel` |
| `grok_upstream_failure.go:406` | `model` |
| `openai_account_runtime_block_fastpath.go:183` | `model` |
| `openai_alpha_search.go:106`、`:185` | `upstreamModel` |
| `openai_embeddings.go:143` | `upstreamModel` |
| `openai_gateway_cc_pipeline.go:137` | `upstreamModel` |
| `openai_gateway_forward.go:1197`、`:1257` | `model`／`upstreamModel` |
| `openai_gateway_passthrough.go:920` | `reqModel` |
| `openai_images.go:678`、`openai_images_responses.go:1921` | `upstreamModel` |

其余 15 个点（含 `gateway_anthropic_passthrough.go` 全部 3 处、`gateway_bedrock.go` 2 处、
`gateway_forward_as_*.go` 2 处、`gemini_*` 2 处、`openai_images_responses.go:2110/:2183` 等）
**作用域内没有任何模型名变量**。

**(2) 其中 4 个是无接收者的自由函数，够不到任何 service 实例：**

```go
func invalidNonStreamingJSONFailoverError(ctx, rateLimitService, resp, account, body, ...)
func grokRetryableOnSameAccount(account *Account, statusCode int, responseBody []byte) bool
func shouldFailoverOpenAIPassthroughResponse(account *Account, statusCode int, responseBody []byte) bool
func openAIStreamFailedEventRetryableOnSameAccount(account *Account, payload []byte, message string) bool
```

要让这 4 个函数查规则，需要给它们加参数或改成方法。这些函数位于官方频繁修改的区域，增加参数或改为方法会扩大同步冲突面。

**因此本设计放弃「改 26 个构造点」**，改为在**同账号重试的唯一裁决点**上做判定。

### 裁决点实测：两族共用同一对判定函数（关键结论，推翻初版设计）

初版设计以为 OpenAI 侧是另一套独立机制，只打算接「族 A」。**实测结论相反**：
两族的循环控制流虽各自内联，但**同账号重试的判定复用同一对包级函数**。

```text
sameAccountRetryAllowed(failoverErr, retryCount, retryLimit)   定义 failover_loop.go:79
effectiveSameAccountRetryLimit(failoverErr, account)           定义 failover_loop.go:113
```

`sameAccountRetryAllowed` 的**非测试调用点共 8 个，全部在 `package handler` 内**：

| # | 调用点 | 所属循环 | 作用域内的模型名变量 |
|---|---|---|---|
| 1 | `failover_loop.go:213` | 族 A：`HandleFailoverError`（间接服务 5 个入口） | 无（经形参传入） |
| 2 | `openai_gateway_handler.go:895` | OpenAI Responses 主循环 | `forwardModel`／`model`／`reqModel` |
| 3 | `openai_gateway_handler.go:1451` | OpenAI Anthropic 兼容循环 | `currentRoutingModel`／`routingModel`／`model`／`reqModel` |
| 4 | `openai_gateway_handler.go:2548` | OpenAI WebSocket（`waitForWSSameAccountRetry` 闭包） | `wsForwardModel`／`model`／`reqModel` |
| 5 | `openai_chat_completions.go:343` | OpenAI Chat Completions | `forwardModel`／`model`／`reqModel` |
| 6 | `openai_images.go:312` | OpenAI Images | `model`／`routingModel` |
| 7 | `openai_alpha_search.go:221` | OpenAI Alpha Search | `model`／`requestedModel` |
| 8 | `grok_media.go:401` | Grok Media | `model`／`routingModel` |

`effectiveSameAccountRetryLimit` 的非测试调用点 **6 个**（第 1、2、3、4、5、6、8 项中的 6 处），
**唯一例外是 `openai_alpha_search.go:220`**：它直接用 `account.GetPoolModeRetryCount()`，
**绕过了 `SameAccountRetryMax` 的错误级封顶**。这是**既有不一致、非本变更引入**，
为控制改动范围，本变更保留这一既有行为，但新 hook 必须在这 8 个点上写法一致。

**因此结论修正为：判定收敛到「一对共享函数」，调用点 8 个，全部同包。**
26 个 `UpstreamFailoverError` 构造点仍然**一行不改**——它们继续产出
`RetryableOnSameAccount` 作为「候选资格」，过滤器在这一对函数里把候选升级或降级。

族 A 的 5 个入口（经 `HandleFailoverError` 间接接入）：

```text
gateway_handler.go:503                   account, reqModel      都在作用域
gateway_handler.go:1038                  account；reqModel 在外层 Messages() 内
gateway_handler_chat_completions.go:304   account；reqModel 在外层 ChatCompletions() 内
gateway_handler_responses.go:292          account；reqModel 在外层 Responses() 内
gemini_v1beta_handler.go:607              account, modelName     都在作用域（同在 GeminiV1BetaModels）
```

### 实测差异一：`terminate` 在两个循环里会被忽略

`terminate` 原计划只靠 `failoverErr.NextAccountAction = NextAccountStop` 表达，
依赖调用方检查 `ShouldRetryNextAccount()`。实测该检查**只有 6 处**：

```text
failover_loop.go:207              族 A         ✓
openai_gateway_handler.go:884     Responses    ✓
openai_gateway_handler.go:1444    Anthropic兼容 ✓
openai_gateway_handler.go:2574    WebSocket    ✓
openai_chat_completions.go:336    ChatCompletions ✓
grok_media.go:391                 Grok Media   ✓
openai_alpha_search.go            ——           ✗ 无此检查
openai_images.go                  ——           ✗ 无此检查
```

`openai_alpha_search.go` 与 `openai_images.go` 的循环**从不检查 `ShouldRetryNextAccount()`**
（`grep` 在这两个文件里 0 命中，且 `:205-240`／`:295-330` 的实际代码里确认：
`ReportOpenAIAccountScheduleResult` → 写出字节检查 → `failoverClientGone` →
直接进 `if failoverErr.RetryableOnSameAccount`）。

**后果**：若只设 `NextAccountStop`，这两条链路会**无视 `terminate`**，继续换号重试。
**处置**：这 2 处必须显式补一次 `ShouldRetryNextAccount()` 检查。这是接入 OpenAI 的
**净增改动点**，不是可选项；漏了就是「页面上配了直接返回、实际仍在换号」的静默不一致。

### 实测差异二：「罚账号」两族不是同一个机制

`switch_account` 要求「换号但别罚这个账号」。实测两族的惩罚路径完全不同：

**族 A —— 临时封禁，窄**

```text
failover_loop.go:238   gatewayService.TempUnscheduleRetryableError(ctx, accountID, failoverErr)
  → gateway_service.go:743 实现：
      !RetryableOnSameAccount        → 直接 return
      RequestScopedTransient         → 直接 return（注释：故障与账号健康无关，封禁只会误摘账号）
      StatusCode == 400              → tempUnscheduleGoogleConfigError
      StatusCode == 502              → tempUnscheduleEmptyResponse
      其余状态码                      → 什么也不做
```

即族 A 只在 **400／502** 两个码上真的封禁。

**族 B —— 熔断器，宽**

```text
ShouldReportAccountScheduleFailure() 门控（6 处：grok_media.go:384、
  openai_chat_completions.go:333、openai_gateway_handler.go:881/:1441/:2570/:3054）
  → ReportOpenAIAccountScheduleResult   openai_account_scheduler.go:2430
      → rateLimitService.ObserveOpenAIAPIKeyHealthFailure
          → openai_apikey_health_breaker.go:53
              窗口内累计失败达阈值 → 构造 TempUnschedState → 冷却 CooldownMinutes 分钟
      → scheduler.ReportResult(accountID, false, firstTokenMs)   负载均衡健康度
```

族 B 是**按失败计数的熔断器**，不挑状态码；且顺带喂给调度器做负载均衡。
`openai_alpha_search.go:207` 与 `openai_images.go:294` 甚至**不经 `ShouldReportAccountScheduleFailure` 门控**，
无条件上报。

**处置（两处一行，对称且易审）**：

```go
// 族 A：gateway_service.go:743 TempUnscheduleRetryableError 内，
//       紧挨既有的 RequestScopedTransient 早退加一条
if failoverErr.SuppressAccountPenalty { return }

// 族 B：gateway_service.go:723 ShouldReportAccountScheduleFailure 内
if e.SuppressAccountPenalty { return false }
```

**必须如实记录的连带影响**：族 B 那一条会同时跳过 `scheduler.ReportResult(..., false, ...)`，
即该次失败**也不计入负载均衡健康度**。语义上与「这个错不是账号的锅」一致，
但它**确实改变了调度器观测到的数据**，不是纯粹的「少罚一次」。
本设计接受该连带影响；若日后发现调度器因此选号偏斜，回看这一段。
`openai_alpha_search.go:207`／`openai_images.go:294` 两处无门控上报**不在本变更修改范围**，
即这两条链路的 `switch_account` **仍会喂一次失败给熔断器** —— 属已知不完整，写进页面说明。

### 可复用的既有规则子系统

「错误透传规则」已经是同一形状、已在生产运行，共 1929 行：

```text
backend/internal/model/error_passthrough_rule.go            96 行  数据模型 + Validate
backend/internal/repository/error_passthrough_repo.go       181 行  CRUD
backend/internal/repository/error_passthrough_cache.go      128 行  Redis + 本地缓存 + PubSub 失效
backend/internal/service/error_passthrough_service.go       387 行  匹配引擎 + 预计算缓存
backend/internal/service/error_passthrough_runtime.go        72 行  运行时适配
backend/internal/handler/admin/error_passthrough_handler.go 282 行  管理端 CRUD
frontend/src/components/admin/ErrorPassthroughRulesModal.vue 646 行  规则编辑 UI
frontend/src/api/admin/errorPassthrough.ts                  137 行  前端 API
```

已具备且可直接照搬的机制：

- `ErrorCodes []int` + `Keywords []string` + `MatchMode "any"|"all"` + `Priority` + `Platforms []string`
- `cachedPassthroughRule`：预计算小写关键词／小写平台／`map[int]struct{}` 码表，避免热路径重复 `ToLower`
- `maxBodyMatchLen = 8 << 10`：正文匹配只看前 8KB（`error_passthrough_service.go:60`），
  并用 `ensureBodyLower` 延迟计算，只在真的需要关键词匹配时才转小写
- 多实例一致性：Redis 缓存 + `NotifyUpdate`／`SubscribeUpdates` 跨实例失效

**本设计不重新发明规则引擎**，`retry_filter_*` 按同一形状实现，并复用上述三条性能约定。

---

## 设计

### 总开关与页面门控

沿用风控中心的既有形状（11 个挂钩点，全部实测定位）：

```text
后端
  SettingKeyRetryFilterEnabled = "retry_filter_enabled"     新增，参照 domain_constants.go:240
  setting_parse.go:220 附近      默认值 "false"
  setting_public.go:244 附近     加入公共设置白名单（前端路由守卫要读）
  setting_public.go:379 附近     RetryControlEnabled: settings[...] == "true"
  setting_update.go:449 附近     管理员改开关时落库
  settings_view.go / dto/settings.go / setting_parse.go:841   三个传输结构体各加字段

前端
  router/index.ts:678 形状        新增路由 + meta.requiresRetryFilter
  router/index.ts:1008 形状       守卫：开关为 false 时 next('/admin/settings')
  router/meta.d.ts:56             声明 requiresRetryFilter?: boolean
  新增 views/admin/RetryFilterView.vue
  设置页新增开关；i18n 仅 zh／en 两份
```

**默认值写在代码里（`setting_parse.go`），不依赖迁移插入。** 风控自己的默认值就是这么来的
（`135_content_moderation.sql` 插了行，但 `setting_parse.go:220` 同时兜底 `"false"`）。

### 数据模型

沿用 `ErrorPassthroughRule` 的字段命名与校验风格。

```go
// backend/internal/model/retry_filter_preset.go
type RetryFilterPreset struct {
    ID          int64
    Name        string
    Enabled     bool
    Description *string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// backend/internal/model/retry_filter_rule.go
type RetryFilterRule struct {
    ID       int64
    PresetID int64
    Name     string
    Enabled  bool
    Priority int      // 数字越小越先判，与 ErrorPassthroughRule 同向

    // ── 条件（四维，均为空列表时视为「不限制该维」）
    StatusCodes    []int    // 维度① HTTP 状态码，精确
    Keywords       []string // 维度② 正文子串，大小写不敏感，前 8KB
    UpstreamCodes  []string // 维度③ 上游错误码字段取值，精确
    UpstreamCodePaths []string // 取值用的 JSON 路径，默认 ["error.type","error.code","code","type"]
    Models         []string // 维度④ 模型名，支持且仅支持 `*` 结尾通配
    MatchMode      string   // "any" | "all"，复用 model.MatchModeAny/All 常量语义

    // ── 动作
    Action         string // same_account_retry | switch_account | terminate | observe
    RetryCountOverride *int    // 仅 same_account_retry 有效；0–50；nil=用账号既有 pool_mode_retry_count
    RetryDelayMs       *int    // 仅 same_account_retry 有效；0–60000ms；nil=用既有退避
    EmptyRequestMode    string // none | body | structured | both
    EmptyResponseMode   string // none | completed | interrupted | both
    TerminateStatusCode *int   // 仅 terminate 有效；nil=透传上游状态码
    TerminateMessage    *string// 仅 terminate 有效；nil=透传上游正文

    Description *string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

`MatchMode` 语义与错误透传完全一致：`all` = 所有**已配置**的维度都必须命中；
`any` = 任一已配置维度命中即可。未配置的维度不参与判定。

**所有错误条件与空事件条件全为空的规则一律校验失败**（沿用 `ErrorPassthroughRule.Validate` 的
「at least one condition」约束），避免出现无条件命中一切的规则。

### 空请求与空响应语义

- `request_empty_body`：HTTP 请求体只有空白或长度为零。
- `request_empty_structured`：JSON 为 `null`、空数组、空对象，或所有已识别输入字段都没有实际内容。
  判定看整个输入集合；一个空字段不得覆盖另一个有效字段，例如 `messages: []` 与有效 `input` 并存时不命中。
- `empty_completed_response`：上游正常给出终止事件，但全程没有文本、工具/函数调用、图像或音频等有效语义输出。
- `empty_interrupted_stream`：HTTP 200/SSE 已建立，但在有效语义输出或正常终止事件前断流。

`usage`、角色声明、心跳、thinking/reasoning-only 事件与终止标记不算语义输出。为保持现有计费契约，携带 usage 的完成事件仍视为有效完成。

### 管理界面与监控布局

左侧使用折叠父菜单“重试过滤器”，子页面为“过滤规则管理”和“重试监控”。规则管理页把预设作为
规则集：规则直接内嵌编辑并支持拖拽排序；账号与分组通过分页搜索、多选窗口绑定，另有全局默认开关。
监控页从既有 Ops JSON 展开事件，提供统计、筛选、分页、自动刷新和详情；事件保存命中时的重试次数、
间隔与空事件类型快照，避免规则后续编辑改变历史展示。

### 三级引用与优先级

```text
账号级   Account.Credentials["retry_filter_preset_id"]      无需迁移
分组级   groups 新增 JSONB 列 retry_filter_config            迁移 240
全局级   设置键 retry_filter_default_preset_id               无需迁移
```

账号级放进 `Credentials` 是**跟随既有惯例**：`pool_mode`、`pool_mode_retry_count`、
`pool_mode_retry_status_codes`、`custom_error_codes` 全部存在这里（`account.go:1092` 起）。
这样账号侧零迁移、零 ent schema 变更。

取值顺序 **账号 → 分组 → 全局，取到第一个非空即用，不跨级合并**。
理由：跨级合并会产生「分组关了某条、账号又开了某条」的叠加语义，管理员无法在页面上
预测最终结果；单级生效可以在页面上直接显示「当前生效的预设：X（来自分组）」。

### 四种动作怎么落地（两族各自的写法）

两族共用同一对判定函数（`failover_loop.go:79` 的 `sameAccountRetryAllowed`、`:113` 的
`effectiveSameAccountRetryLimit`，见「裁决点实测」），但**循环控制流各自内联**。
所以同一个动作在两族的落地写法不完全相同，必须分别写清。

**族 A 现状**（`HandleFailoverError` 在 `failover_loop.go:193`、`ShouldRetryNextAccount()`
检查在 `:207`、`sameAccountRetry` 赋值在 `:213`、临时封禁在 `:238`）：

```go
sameAccountRetry := sameAccountRetryAllowed(failoverErr, retryCount, retryLimit)
if sameAccountRetry { ...sleep...; return FailoverContinue }
if failoverErr.RetryableOnSameAccount { gatewayService.TempUnscheduleRetryableError(...) }
s.FailedAccountIDs[accountID] = struct{}{}
if s.SwitchCount >= s.MaxSwitches { return FailoverExhausted }
s.SwitchCount++
return FailoverContinue
```

族 A 的重试计数存在**共享结构体** `FailoverState` 上（`SameAccountRetryCount map[int64]int`，
`failover_loop.go:129`），`retryLimit` 是 `HandleFailoverError` 的**入参**，由调用方算好传进来。

**族 B 现状**（形状取自 Responses 主循环 `openai_gateway_handler.go:893-925`）：

```go
// 重试计数是循环内的局部 map，不经 FailoverState
if failoverErr.RetryableOnSameAccount {
    retryLimit := effectiveSameAccountRetryLimit(failoverErr, account)   // 循环自己算
    if sameAccountRetryAllowed(failoverErr, sameAccountRetryCount[account.ID], retryLimit) {
        sameAccountRetryCount[account.ID]++
        ...sleep...
        continue
    }
}
h.gatewayService.RecordOpenAIAccountSwitch()
failedAccountIDs[account.ID] = struct{}{}
if switchCount >= maxAccountSwitches { h.handleFailoverExhausted(...); return }
switchCount++
```

即族 B 每条链路**自己声明 `sameAccountRetryCount := make(map[int64]int)`、自己 `switchCount++`、
自己调 `handleFailoverExhausted`**，没有共享状态结构体。7 处声明点实测：

```text
openai_gateway_handler.go:624    Responses 主循环
openai_gateway_handler.go:1256   Anthropic 兼容
openai_gateway_handler.go:2539   WebSocket
openai_chat_completions.go:156   Chat Completions
openai_images.go:153             Images
openai_alpha_search.go:116       Alpha Search
grok_media.go:186                Grok Media
```

逐动作映射：

| 动作 | 族 A（`failover_loop.go` 一处，覆盖 5 个入口） | 族 B（7 个内联循环） |
|---|---|---|
| `same_account_retry`（默认） | 让 `sameAccountRetryAllowed` 返回 true。`RetryCountOverride` 非空时替换 `retryLimit` —— 它已是 `HandleFailoverError` 的入参，改入参即可 | 同样让判定返回 true。但 `retryLimit` 是循环内 `effectiveSameAccountRetryLimit(failoverErr, account)` 的返回值，`RetryCountOverride` 必须在这一步生效。**`openai_alpha_search.go:220` 例外**：它直接用 `account.GetPoolModeRetryCount()`、绕过 `SameAccountRetryMax` 封顶，所以这条链路上 `RetryCountOverride` 的生效路径与其余 6 处不同（既有不一致，本变更不改这一行） |
| `switch_account` | 让判定返回 false，**并置 `SuppressAccountPenalty = true`**。惩罚是 `TempUnscheduleRetryableError`，只在 400／502 两个状态码上真封禁 | 让判定返回 false，**并置 `SuppressAccountPenalty = true`**。惩罚是 `ShouldReportAccountScheduleFailure` → `ReportOpenAIAccountScheduleResult` → 熔断器（按窗口内失败计数，不挑状态码）+ 调度器负载均衡健康度 |
| `terminate` | 置 `NextAccountAction = NextAccountStop`。`failover_loop.go:207` 已有 `ShouldRetryNextAccount()` 检查，直接 `FailoverExhausted` | 同样置 `NextAccountStop`，但只有 5 处有该检查（`openai_gateway_handler.go:884`／`:1444`／`:2574`、`openai_chat_completions.go:336`、`grok_media.go:391`）。**`openai_alpha_search.go` 与 `openai_images.go` 完全没有，必须各补一次检查，否则 `terminate` 静默失效** |
| `observe` | 判定照跑、只记录，不改任何返回值 | 与族 A 一致：判定照跑、只记录，不改任何返回值 |

`NextAccountStop` 是既有取值（`gateway_service.go:678`），且 `openai_gateway_upstream_errors.go:339/:349`
已经在用 `NextAccountRetry`，说明这套 tri-state 就是为这类短路准备的。

`TerminateStatusCode`／`TerminateMessage` 的实际覆写位置在两族各自的「耗尽处置」路径上
（族 A 的 `FailoverExhausted` 返回后由调用方处理，族 B 的 `handleFailoverExhausted`），
设计阶段尚未核验这两条路径的取值顺序，需在实现时确认。

**落点与连带影响见前文「实测差异一」「实测差异二」与开放问题 (a)**：
`SuppressAccountPenalty` 为新增字段，两族各一行早退的具体写法、族 B 会同时跳过
负载均衡健康度上报的连带影响、以及 `openai_alpha_search.go` 与 `openai_images.go`
两处补检查的净增改动，均已在那几节登记，本节只作指针，不重复论述。

### 签名变更与调用一致性

裁决点需要 `account`（拿 `Credentials` 里的预设引用）与模型名。实测：

- 族 A 的 5 个调用点**全部有 `account` 在作用域**；`gateway_handler.go:503` 与
  `gemini_v1beta_handler.go:607` 直接有模型名，另 3 个的模型名在外层函数
  （`Messages()`／`ChatCompletions()`／`Responses()` 的 `reqModel`）内，传下来即可。
- 现签名 `HandleFailoverError(ctx, gatewayService, accountID int64, platform string, retryLimit int, failoverErr)`
  已经在收 `retryLimit`，调用方算法是 `account.GetPoolModeRetryCount()`。

拟改为收一个入参结构体而不是继续加形参：

```go
type FailoverDecisionInput struct {
    Account     *service.Account
    Model       string
    Platform    string
    RetryLimit  int                  // 调用方仍传 account.GetPoolModeRetryCount()，作为未命中规则时的回落
    FailoverErr *service.UpstreamFailoverError
}
```

这是一处跨 5 个调用点的签名变更，需核对所有调用点的形参与实参一致性。人工核对不能替代编译验证。

### 族 B 接入范围

v1 同时接入族 A 与 OpenAI 族。

v1 覆盖范围（8 个挂钩点，全部在 `package handler`）：

| 链路 | 挂钩点 | 备注 |
|---|---|---|
| Anthropic passthrough／Bedrock／`/v1/messages`／`/v1/chat/completions`／`/v1/responses`／Gemini v1beta | 族 A 经 `failover_loop.go:213`（5 个入口） | 一处改动覆盖 5 个入口 |
| OpenAI Responses 主循环 | `openai_gateway_handler.go:895` | |
| OpenAI Anthropic 兼容 | `openai_gateway_handler.go:1451` | |
| OpenAI WebSocket | `openai_gateway_handler.go:2548` | 闭包 `waitForWSSameAccountRetry`；**只处理 429 且要求 `SameAccountRetryDeadline` 非零**，过滤器不得放宽该前置 |
| OpenAI Chat Completions | `openai_chat_completions.go:343` | |
| OpenAI Images | `openai_images.go:312` | **需补 `ShouldRetryNextAccount()` 检查** |
| OpenAI Alpha Search | `openai_alpha_search.go:221` | **需补 `ShouldRetryNextAccount()` 检查**；且此处用的是 `GetPoolModeRetryCount()` 而非 `effectiveSameAccountRetryLimit` |
| Grok Media | `grok_media.go:401` | |

`openai_gateway_handler.go` 是官方高频改动文件，v1 在其中有 3 个挂钩点，后续同步可能产生冲突。为缩小冲突面，每个挂钩点只调整判定函数参数，不重排、不抽函数、不改变周边控制流。

### 观察模式记到哪：不新增表

`observe` 与「命中即生效」的规则都需要留痕。现成载体：

```text
backend/internal/service/ops_upstream_context.go:380   OpsUpstreamErrorEvent
```

它是**请求级 JSON**（`appendOpsUpstreamError` 挂在 `gin.Context` 上），
已含 `Kind`／`Stage`／`Scope`／`Reason`／`Message`／`Detail`，**存入 JSON、不涉及 DB schema 变更**
（`Passthrough` 字段的注释原文即如此声明）。

因此：

- 命中记录走**结构化日志** `gateway.retry_filter_match`（zap，形状照 `failover_loop.go:223` 的
  `gateway.failover_same_account_retry`：`account_id`／`upstream_status`／规则 ID／动作）。
- 另在 ops 事件上追加规则 ID 与动作两个 JSON 字段，复用既有落库通道。
- **不新增审计表、不新增迁移。** 观察模式的价值是「启用前空跑看命中」，日志与 ops 面板足够。

### 迁移编号

当前最大为 238，且**官方自己存在两个 238**：

```text
238_opencode_go_platform.sql               242907854 刘佳林   2026-09-11
238_purge_unlimited_user_platform_quotas.sql 4a4b50d80 feeeei  2026-09-10
```

两者均为官方提交、均已在线上运行，**不是本项目造成的重号**，本变更不修正它。
本变更取 **239**（两张新表）与 **240**（`groups.retry_filter_config`）。

### 权限边界

- 全部路由挂 `/api/v1/admin/*`，参照 `backend/internal/server/routes/admin.go:171` 的
  `admin.Group("/risk-control")` 形状新增 `admin.Group("/retry-filter")`。
- **不提供任何 `/api/v1/my/*` 入口**：重试策略影响调度与上游额度，是管理员职责。
- 前端页面只在管理员菜单出现；路由 meta 同时要求 `requiresAdmin` 与 `requiresRetryFilter`。
- 预设与规则**不含任何凭据字段**，不存 URL、Token、代理认证。

---

## 开放问题

**(a) 新增 `SuppressAccountPenalty bool`。**
不复用 `RequestScopedTransient` —— 后者注释明写「可同账号重试」，与 `switch_account`
（不在本账号重试）语义相反，硬套会误导后续读代码的人。
落点见「实测差异二」：`TempUnscheduleRetryableError` 与 `ShouldReportAccountScheduleFailure`
各加一行。这是本变更对 `UpstreamFailoverError` 的**唯一**结构改动。
**连带影响**：族 B 会同时跳过负载均衡健康度上报。

**(b) v1 同时接入 OpenAI 族。**
见「族 B 接入范围」。净增两项必做改动：`openai_images.go` 与 `openai_alpha_search.go`
各补一次 `ShouldRetryNextAccount()` 检查，否则 `terminate` 在这两条链路上静默失效。

**(c) 为什么 v1 不做正则。**
正则会带来两个已知风险：一是管理员可写出灾难性回溯的表达式，在**每次上游报错的热路径**上执行；
二是 `ErrorPassthroughRule` 至今只用子串，引入正则会让两套规则的表达能力不一致、
管理员容易混淆。子串 + `*` 后缀通配覆盖了「按上游文案分类」的绝大多数场景。
v2 若要加，应当带超时与复杂度上限。

**(d) 维度③的 JSON 路径默认值是否够用。**
默认取 `error.type`／`error.code`／`code`／`type`。这是按 Anthropic
（`{"type":"error","error":{"type":"...","message":"..."}}`，见 `gateway_service.go:730` 起
`sseStreamErrorEventError` 的注释）与 OpenAI 常见形状拟的，**未逐平台实测**。
上游正文形状差异需要使用各平台错误样本核验。

**(e) 新增：WebSocket 挂钩点的前置条件是否保留。**
`waitForWSSameAccountRetry`（`openai_gateway_handler.go:2543-2548`）在调用判定函数前
**先要求 `StatusCode == 429` 且 `SameAccountRetryDeadline` 非零**。
本设计**保留**该前置，即过滤器在 WS 链路上**不能**把非 429 错误升级为同账号重试。
理由：WS 连接的重试语义与 HTTP 不同（要重放 `firstMessage`、要重新抢占并发槽），
放宽前置等于改 WS 会话恢复语义，超出「重试过滤」的职责。**页面须对 WS 链路标注此限制。**
若日后要放宽，属独立变更。

## 不做的事

- 不改那 26 个 `UpstreamFailoverError` 构造点。
- 不改 `IsPoolModeRetryableStatus`、不废弃账号上的两个池模式旋钮、不做数据迁移。
- 不引入正则、不引入脚本／表达式语言。
- 不新增审计表。
- 不对普通用户开放。
