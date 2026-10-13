## Purpose

规定「重试过滤」在网关链路上的判定位置、生效范围、四种动作在两族循环上的落地语义、
账号健康惩罚的抑制及其已知不完整之处、WebSocket 链路的前置约束、观察模式的留痕方式，
以及默认关闭时的等价性保证。配置面的数据模型与管理端见 `retry-filter-rules`。

本规范描述系统**应当**满足的行为，不声称任何行为已经通过验证。

## ADDED Requirements

### Requirement: 判定收敛在同账号重试的裁决点，不铺在错误构造点

系统 MUST 在同账号重试的裁决点执行规则判定，MUST NOT 在构造上游 failover 错误的各个位置
分别判定。既有的 `RetryableOnSameAccount` 产出 MUST 保持不变，MUST 被视为「候选资格」；
过滤器 MUST 只在裁决点把候选升级或降级。

判定 MUST 收敛到同账号重试判定与同账号重试上限计算这一对共享函数上，
两族循环 MUST 共用同一份判定实现，MUST NOT 各自复制一份判定逻辑。

系统 MUST NOT 修改既有的 `Account.IsPoolModeRetryableStatus` 判定，
MUST NOT 废弃或迁移账号上既有的 `pool_mode_retry_count` 与 `pool_mode_retry_status_codes`。

#### Scenario: 构造点行为不变

- **WHEN** 上游返回 429，账号处于池模式且该状态码在账号配置内
- **THEN** 构造出的 failover 错误 MUST 仍然标记为可同账号重试
- **THEN** 最终是否重试 MUST 由裁决点的规则判定结果决定

#### Scenario: 未命中任何规则时回落现状

- **WHEN** 总开关为真、存在生效预设，但本次错误未命中其中任何规则
- **THEN** 系统 MUST 按账号既有的池模式配置决定是否重试及重试次数
- **THEN** 判定结果 MUST 与总开关关闭时一致

#### Scenario: 两族共用一份判定实现

- **WHEN** 检索非测试代码中同账号重试判定函数的调用点
- **THEN** 每个调用点 MUST 经由同一对共享函数得到判定结果
- **THEN** MUST NOT 存在绕过该对函数、自行判定规则动作的链路

### Requirement: v1 覆盖两族共 8 个挂钩点

v1 MUST 覆盖以下全部挂钩点，每一处 MUST 应用同一套规则判定与动作语义：

1. 统一 failover 裁决点（族 A），MUST 覆盖其间接服务的 5 个入口：Anthropic passthrough／Bedrock／
   `/v1/messages`／`/v1/chat/completions`／`/v1/responses`／Gemini v1beta
2. OpenAI Responses 主循环
3. OpenAI Anthropic 兼容循环
4. OpenAI WebSocket（同账号重试等待闭包）
5. OpenAI Chat Completions 循环
6. OpenAI Images 循环
7. OpenAI Alpha Search 循环
8. Grok Media 循环

第 2–8 项属族 B，其重试计数与切换计数 MUST 保持各自循环内既有的局部状态，
本变更 MUST NOT 为接入过滤器而把族 B 的循环状态改为共享结构体。

每个挂钩点的改动 MUST 限定在被调用的判定函数及其入参上，MUST NOT 重排周边控制流、
MUST NOT 抽取或合并既有循环。

#### Scenario: 8 个挂钩点逐一可验证

- **WHEN** 对上述任一挂钩点构造一次能命中某条规则的上游错误
- **THEN** 该挂钩点 MUST 应用该规则判定出的动作
- **THEN** 动作语义 MUST 与其余挂钩点一致

#### Scenario: 未接入的判定路径不存在

- **WHEN** 检索非测试代码中同账号重试判定函数的全部调用点
- **THEN** 每个调用点 MUST 已接入规则判定
- **THEN** MUST NOT 存在仅按状态码判定、未经规则判定的剩余调用点

### Requirement: 判定所需数据不得引发额外上游请求或额外读流

判定使用的状态码、响应正文与模型名 MUST 取自请求处理过程中已经获得的数据——构造上游 failover
错误时正文已被读入并复位。系统 MUST NOT 为判定发起任何新的上游请求，
MUST NOT 重新读取已被消费的响应体，MUST NOT 影响流式响应的转发时序。

正文关键词与上游错误码的匹配 MUST 只检查正文的前若干字节（上限 MUST 与既有错误透传规则一致），
且 MUST 仅在确实需要该维度时才做大小写转换。

#### Scenario: 判定不增加上游请求

- **WHEN** 一次上游错误触发规则判定
- **THEN** 该次请求的上游请求计数 MUST NOT 因判定而增加

#### Scenario: 超长正文只匹配前缀

- **WHEN** 上游返回的错误正文远超正文匹配上限，且关键词只出现在上限之后
- **THEN** 该关键词维度 MUST 判定为不命中

#### Scenario: 缺少正文时不阻塞判定

- **WHEN** 本次 failover 错误未携带响应正文
- **THEN** 正文关键词与上游错误码两个维度 MUST 判定为不命中
- **THEN** 系统 MUST NOT 为取得正文而读取上游

#### Scenario: 流式响应不受影响

- **WHEN** 流式请求在已向客户端写出内容后发生上游错误并触发判定
- **THEN** 客户端已收到的字节 MUST NOT 被改变
- **THEN** 既有的「写出后是否允许切换账号」约束 MUST 继续生效

### Requirement: 判定所需的账号与模型名经入参传入，不新增全局状态

裁决点 MUST 通过入参获得账号与请求模型名，MUST NOT 依赖包级变量、
MUST NOT 依赖从 `gin.Context` 隐式取值来得到这两项。

模型名在部分挂钩点的判定点作用域内不可直接见，此类挂钩点 MUST 由外层函数把模型名传入；
模型名确实不可得时，模型名维度 MUST 判定为不命中，MUST NOT 判定为命中一切。

#### Scenario: 模型名缺失时不误命中

- **WHEN** 某挂钩点本次判定无法取得请求模型名，而规则配置了模型名维度
- **THEN** 该模型名维度 MUST 判定为不命中
- **THEN** `all` 模式的规则 MUST 整条不命中

### Requirement: v1 生效范围限定为池模式账号，且范围本身可配置

本能力 v1 MUST 只对池模式账号生效，对非池模式账号 MUST 表现为完全不生效。
生效范围 MUST 实现为配置项，MUST NOT 在代码中硬编码「只有池模式」这一假设；
后续扩大范围 MUST 只需增加范围取值，MUST NOT 需要重构判定入口。

#### Scenario: 非池模式账号不受影响

- **WHEN** 总开关为真、存在能命中的规则，但账号未启用池模式
- **THEN** 系统 MUST NOT 应用任何规则动作
- **THEN** 该账号的重试行为 MUST 与本变更引入前一致

#### Scenario: 范围为配置项

- **WHEN** 检查判定入口的实现
- **THEN** 生效范围 MUST 来自可配置取值，MUST NOT 是内联的池模式条件判断

### Requirement: 总开关关闭时行为与现状等价

总开关为 `false` 时，系统 MUST NOT 读取任何预设或规则，MUST NOT 产生任何判定开销，
MUST NOT 设置账号惩罚抑制标记，MUST NOT 改变任何一个挂钩点的判定结果。
八个挂钩点的重试次数、是否切换账号、是否施加账号惩罚、返回给客户端的状态码与正文
MUST 与本变更引入前逐项一致。

#### Scenario: 开关关闭时八个挂钩点均无变化

- **WHEN** 总开关为 `false`，在任一挂钩点发生上游可重试错误
- **THEN** 同账号重试次数 MUST 取自账号既有池模式配置
- **THEN** 账号惩罚路径 MUST 按本变更引入前的条件触发
- **THEN** 系统 MUST NOT 查询规则缓存或数据库

#### Scenario: 开关关闭时不写入抑制标记

- **WHEN** 总开关为 `false`，上游返回可重试错误且同账号重试次数耗尽
- **THEN** failover 错误上的账号惩罚抑制标记 MUST 保持为假
- **THEN** 既有的临时封禁与熔断上报 MUST 按原条件执行

### Requirement: same_account_retry 的落地语义

动作为 `same_account_retry` 时，系统 MUST 在当前账号重试。
规则提供重试次数覆盖值时 MUST 使用该值作为上限，未提供时 MUST 使用账号既有配置。
规则提供重试间隔时 MUST 在每次同账号重试前使用该固定间隔；允许值为 0–60000ms，
未提供时 MUST 保留既有的上游指定延迟或指数退避。

既有的错误级重试上限、重试截止时间与重试间隔退避 MUST 继续生效，
规则的覆盖值 MUST NOT 突破错误自身携带的更严格上限。

覆盖值在两族的生效位置不同：族 A 的重试上限是裁决点入参，覆盖值 MUST 在该入参上生效；
族 B 的重试上限由各循环自行计算，覆盖值 MUST 在该计算处生效。
除下述既有例外之外，各挂钩点 MUST 在相同输入下得到相同的最终上限。

**既有例外，本变更不修正**：OpenAI Alpha Search 链路的重试上限直接取自账号池模式重试次数，
不经共享的上限计算函数，因此 MUST NOT 假定该链路会应用错误自身携带的更严格上限。
覆盖值在该链路 MUST 仍然生效，但其与错误级上限的相互作用 MUST 被视为与其余挂钩点不同。
修正该不一致 MUST 作为独立变更处理。

#### Scenario: 覆盖值不突破错误级上限

- **WHEN** 规则覆盖值为 8，而本次错误自身携带的同账号重试上限为 2
- **THEN** 实际同账号重试次数上限 MUST 为 2

#### Scenario: 覆盖值为 0 时不重试

- **WHEN** 规则覆盖值为 0
- **THEN** 系统 MUST NOT 在当前账号重试
- **THEN** 系统 MUST 继续按切换账号路径处理

#### Scenario: 自定义零间隔

- **WHEN** 规则重试间隔为 0ms 且仍有同账号重试预算
- **THEN** 系统 MUST 立即发起下一次同账号尝试

#### Scenario: 除既有例外外各挂钩点得到相同上限

- **WHEN** 同一条带覆盖值的规则分别在族 A 与族 B 的挂钩点上命中，账号配置相同
- **THEN** 除 OpenAI Alpha Search 之外的各挂钩点计算出的同账号重试上限 MUST 相同

#### Scenario: Alpha Search 链路的上限差异不被掩盖

- **WHEN** 一条带覆盖值的规则在 OpenAI Alpha Search 链路上命中，且本次错误携带更严格的错误级上限
- **THEN** 该链路 MAY 不应用该错误级上限
- **THEN** 该差异 MUST 被如实记录为既有不一致，MUST NOT 被表述为本变更已修正

### Requirement: switch_account 必须跳过账号健康惩罚

动作为 `switch_account` 时，系统 MUST NOT 在当前账号重试，MUST 直接进入切换账号路径，
并 MUST NOT 因本次错误对该账号施加临时封禁或调度健康惩罚。

该「跳过惩罚」MUST 通过 failover 错误上新增的显式抑制标记表达，
MUST NOT 复用语义为「允许同账号重试」的既有标记。抑制 MUST 在两处生效：
族 A 的临时封禁入口、族 B 的调度失败上报门控。

抑制族 B 的失败上报 MUST 被如实记录为同时跳过调度器负载均衡健康度上报——
该次失败不计入负载均衡观测数据，这是本变更接受的连带影响，MUST NOT 被描述为「仅少罚一次」。

本变更 MUST NOT 修改 OpenAI Alpha Search 与 OpenAI Images 两条链路上不经门控的失败上报。
因此这两条链路的 `switch_account` **仍会**向熔断器喂一次失败，MUST 作为已知不完整
在管理端页面说明中明示，MUST NOT 声称这两条链路已完全免除惩罚。

#### Scenario: 族 A 不触发临时封禁

- **WHEN** 一条 `switch_account` 规则在族 A 挂钩点命中，且上游状态码属于会触发临时封禁的取值
- **THEN** 当前账号 MUST NOT 被临时置为不可调度
- **THEN** 当前账号 MUST 仍可被后续请求选中

#### Scenario: 族 B 不触发熔断器失败计数

- **WHEN** 一条 `switch_account` 规则在经门控上报的族 B 挂钩点命中
- **THEN** 该次失败 MUST NOT 计入账号熔断器的窗口失败计数
- **THEN** 该账号 MUST NOT 因本次错误进入冷却

#### Scenario: 连带影响如实成立

- **WHEN** 族 B 的失败上报因抑制标记被跳过
- **THEN** 调度器负载均衡健康度 MUST NOT 收到本次失败结果
- **THEN** 该行为 MUST 与页面说明及设计记录一致

#### Scenario: 两条链路的已知不完整必须明示

- **WHEN** 一条 `switch_account` 规则在 OpenAI Alpha Search 或 OpenAI Images 链路上命中
- **THEN** 系统 MUST 仍然切换账号且 MUST NOT 在当前账号重试
- **THEN** 该次失败 MAY 仍被上报给熔断器
- **THEN** 管理端页面说明 MUST 明示这两条链路的惩罚抑制不完整

#### Scenario: 切换计数与耗尽判定不变

- **WHEN** 因 `switch_account` 发生账号切换
- **THEN** 切换计数 MUST 按各自循环既有规则递增
- **THEN** 切换次数达到上限时 MUST 按既有的耗尽路径向客户端返回错误

### Requirement: terminate 直接返回，不重试不切换

动作为 `terminate` 时，系统 MUST NOT 在当前账号重试，MUST NOT 切换账号，
MUST 直接结束本次请求并向客户端返回错误。

`terminate` MUST 通过既有的「不再尝试下一账号」取值表达，MUST NOT 新增控制流分支。
每个挂钩点 MUST 在切换账号之前检查该取值；不具备该检查的链路 MUST 补齐检查，
MUST NOT 依赖其他挂钩点的检查代为生效。

未配置自定义状态码时 MUST 透传上游状态码；未配置自定义消息时 MUST 返回上游正文
（MUST 经过既有脱敏处理）。自定义消息 MUST NOT 包含任何凭据或上游节点信息。

#### Scenario: 命中后不再选号

- **WHEN** 一条 `terminate` 规则命中，且分组内仍有其他可用账号
- **THEN** 系统 MUST NOT 选择其他账号
- **THEN** 客户端 MUST 立即收到错误响应

#### Scenario: Alpha Search 与 Images 链路必须真的终止

- **WHEN** 一条 `terminate` 规则在 OpenAI Alpha Search 链路上命中，且仍有其他可用账号
- **THEN** 系统 MUST 立即结束本次请求
- **THEN** 系统 MUST NOT 继续切换账号重试
- **WHEN** 同一条规则在 OpenAI Images 链路上命中，且仍有其他可用账号
- **THEN** 系统 MUST 立即结束本次请求
- **THEN** 系统 MUST NOT 继续切换账号重试

#### Scenario: 自定义状态码与消息生效

- **WHEN** 规则配置自定义状态码 400 与自定义消息，上游返回 429
- **THEN** 客户端 MUST 收到状态码 400 与该自定义消息
- **THEN** 客户端 MUST NOT 收到上游原始正文

### Requirement: WebSocket 链路保留既有前置条件，过滤器不得放宽

OpenAI WebSocket 挂钩点 MUST 保留其既有前置条件：仅在上游状态码为 429
且同账号重试截止时间非零时才进入同账号重试判定。

过滤器 MUST NOT 在该链路上把不满足前置条件的错误升级为同账号重试，
即便规则动作为 `same_account_retry`。放宽该前置 MUST 作为独立变更处理。

管理端页面 MUST 对 WebSocket 链路标注此限制。

#### Scenario: 非 429 错误不得被升级为同账号重试

- **WHEN** WebSocket 链路上游返回 500，且一条动作为 `same_account_retry` 的规则能命中
- **THEN** 系统 MUST NOT 在当前账号重试
- **THEN** 系统 MUST 按该链路既有路径处理该错误

#### Scenario: 缺少重试截止时间时不得被升级

- **WHEN** WebSocket 链路上游返回 429，但同账号重试截止时间为零值
- **THEN** 系统 MUST NOT 在当前账号重试

#### Scenario: 其余动作在 WS 链路仍然生效

- **WHEN** WebSocket 链路满足既有前置条件，且一条 `switch_account` 或 `terminate` 规则命中
- **THEN** 该动作 MUST 按本规范的对应语义落地

### Requirement: observe 只记录不干涉

动作为 `observe` 时，系统 MUST 记录本次命中，并 MUST NOT 改变本次请求的任何行为：
是否同账号重试、重试次数、是否切换账号、是否施加账号惩罚、返回给客户端的内容
MUST 与未命中任何规则时完全一致。

`observe` MUST NOT 设置账号惩罚抑制标记，MUST NOT 设置「不再尝试下一账号」取值。
判定 MUST 照常执行至得出动作，MUST NOT 因动作为 `observe` 而提前跳过匹配。

#### Scenario: 观察模式不改变行为

- **WHEN** 一条 `observe` 规则命中
- **THEN** 本次请求的重试与切换行为 MUST 与总开关关闭时一致
- **THEN** 系统 MUST 产生一条命中记录

#### Scenario: 观察模式不抑制惩罚

- **WHEN** 一条 `observe` 规则命中，且本次错误在现状下会触发临时封禁或熔断上报
- **THEN** 该惩罚 MUST 照常发生
- **THEN** 命中记录 MUST 标明本次动作未改变行为

### Requirement: 命中留痕复用既有通道，不新增审计表

命中记录 MUST 通过结构化日志输出，并 MUST 追加到既有的运维上游错误事件的 JSON 字段中。
留痕 MUST NOT 新增数据库表，MUST NOT 新增数据库列，MUST NOT 新增数据库迁移。

留痕 MUST 至少包含账号标识、上游状态码、命中的预设与规则标识、判定出的动作、
以及该动作是否实际改变了行为。留痕 MUST NOT 包含令牌、代理认证、订阅地址或完整上游正文。

#### Scenario: 留痕字段完整

- **WHEN** 任一规则命中
- **THEN** 日志 MUST 含账号标识、上游状态码、预设与规则标识、动作
- **THEN** 运维上游错误事件 MUST 含规则标识与动作

#### Scenario: 留痕不新增存储结构

- **WHEN** 检查本变更的数据库迁移
- **THEN** MUST 不存在为命中留痕新增的表或列

#### Scenario: 留痕不含敏感信息

- **WHEN** 检查命中留痕的输出
- **THEN** MUST 不含令牌、代理认证或订阅地址
- **THEN** MUST 不含未经截断与脱敏的上游正文

### Requirement: 判定失败时不得影响请求

规则查询、缓存读取或判定过程发生任何错误时，系统 MUST 降级为「未命中任何规则」，
MUST 按账号既有池模式配置继续处理，MUST NOT 使请求失败，MUST NOT 阻塞请求，
MUST NOT 设置账号惩罚抑制标记或「不再尝试下一账号」取值。

#### Scenario: 缓存不可用时降级

- **WHEN** 判定所需的规则缓存不可用且数据库查询失败
- **THEN** 本次请求 MUST 按账号既有池模式配置继续处理
- **THEN** 客户端 MUST NOT 因此收到额外错误
- **THEN** 系统 MUST 记录一条降级日志

#### Scenario: 降级不得残留半生效状态

- **WHEN** 判定在得出动作之前失败
- **THEN** failover 错误上的抑制标记与「不再尝试下一账号」取值 MUST 保持判定前的原值
