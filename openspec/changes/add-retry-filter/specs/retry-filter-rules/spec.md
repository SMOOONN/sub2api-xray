## Purpose

规定「重试过滤」的配置面：总开关、预设（模板）与规则的数据模型、校验规则、三级引用与取值优先级、
缓存与多实例一致性、管理端接口与页面门控、权限边界。运行时如何判定与落地动作见
`retry-filter-runtime`。

本规范描述系统**应当**满足的行为，不声称任何行为已经通过验证。

## ADDED Requirements

### Requirement: 功能默认关闭，且关闭时行为与现状等价

系统 MUST 提供总开关设置键 `retry_filter_enabled`，其默认值 MUST 为 `false`，
且该默认值 MUST 由后端代码提供，MUST NOT 仅依赖数据库迁移插入的行。
升级既有实例时该开关 MUST 保持 `false`，MUST NOT 因升级自动启用。

开关为 `false` 时，系统 MUST NOT 读取任何预设或规则，MUST NOT 产生任何判定开销，
同账号重试的判定结果 MUST 与本变更引入前完全一致。

该开关 MUST 进入公共设置白名单，使前端路由守卫可在未登录管理端接口的情况下读取其取值。

#### Scenario: 升级既有实例后开关保持关闭

- **WHEN** 一个此前运行旧版本、数据库中不存在 `retry_filter_enabled` 行的实例升级到本版本
- **THEN** 系统 MUST 将该开关视为 `false`
- **THEN** 系统 MUST NOT 向客户端或管理员报告任何配置错误

#### Scenario: 开关关闭时不触碰规则存储

- **WHEN** 开关为 `false`，且数据库中存在若干启用状态的预设与规则
- **THEN** 上游返回可重试错误时，系统 MUST NOT 查询规则缓存或数据库
- **THEN** 同账号重试的次数与是否重试 MUST 与账号既有 `pool_mode_retry_count`
  和 `pool_mode_retry_status_codes` 的结果一致

### Requirement: 独立管理页面，且受总开关门控

系统 MUST 提供独立的管理端页面用于编排重试过滤，MUST NOT 把该配置嵌入账号编辑或分组编辑弹窗。
该页面的路由 MUST 同时要求管理员身份与总开关为真；开关为 `false` 时直达该路由
MUST 被重定向到管理端设置页，MUST NOT 渲染页面内容。

管理端设置页 MUST 提供该总开关的开启入口。页面文案 MUST 只提供中文与英文两份。

#### Scenario: 开关关闭时直达页面被拦截

- **WHEN** 管理员在开关为 `false` 时直接访问重试过滤页面的 URL
- **THEN** 系统 MUST 重定向到管理端设置页
- **THEN** 系统 MUST NOT 请求任何重试过滤的管理端接口

#### Scenario: 开关打开后页面可进入

- **WHEN** 管理员在设置页把总开关置为真后访问重试过滤页面
- **THEN** 页面 MUST 正常渲染
- **THEN** 系统 MUST NOT 要求重启进程或重新登录

#### Scenario: 普通用户无法访问

- **WHEN** 已登录的普通用户访问重试过滤页面的 URL
- **THEN** 系统 MUST 拒绝访问
- **THEN** 系统 MUST NOT 在普通用户的任何导航菜单中展示该页面入口

### Requirement: 规则的上游错误与空事件判定维度

规则 MUST 支持以下四个条件维度，每一维均可为空，为空表示「该维不参与判定」：

1. HTTP 状态码列表，精确匹配，取值范围 MUST 限定为 100–599
2. 上游响应正文关键词列表，子串匹配，MUST 大小写不敏感，MUST 只检查正文前 8KB
3. 上游自带错误码列表，取值来自响应正文的若干 JSON 路径，精确匹配
4. 请求模型名列表，MUST 支持且仅支持以 `*` 结尾的后缀通配
5. 空请求模式：`none`、`body`、`structured`、`both`
6. 空响应模式：`none`、`completed`、`interrupted`、`both`

规则 MUST 提供匹配模式字段，取值 `any` 或 `all`，语义 MUST 与既有错误透传规则一致：
`all` 表示所有**已配置**的维度都必须命中；`any` 表示任一已配置维度命中即可。
未配置的维度 MUST NOT 参与判定。

系统 MUST NOT 支持正则表达式，MUST NOT 支持脚本或表达式语言。

#### Scenario: all 模式下未配置的维度不参与判定

- **WHEN** 一条 `all` 模式规则只配置了状态码 `[429]` 与关键词 `["quota"]`，模型名与上游错误码均为空
- **THEN** 上游返回 429 且正文含 `quota` 时该规则 MUST 命中
- **THEN** 系统 MUST NOT 因模型名与上游错误码为空而判定为不命中

#### Scenario: any 模式下单维命中即成立

- **WHEN** 一条 `any` 模式规则配置了状态码 `[429]` 与关键词 `["quota"]`
- **THEN** 上游返回 429 但正文不含 `quota` 时该规则 MUST 命中

#### Scenario: 关键词匹配大小写不敏感

- **WHEN** 规则关键词为 `["Quota Exhausted"]`，上游正文为 `{"error":{"message":"QUOTA EXHAUSTED"}}`
- **THEN** 该关键词维度 MUST 判定为命中

#### Scenario: 模型名后缀通配

- **WHEN** 规则模型名为 `["claude-opus-*"]`，请求模型为 `claude-opus-4-20250514`
- **THEN** 该模型名维度 MUST 判定为命中
- **WHEN** 请求模型为 `claude-sonnet-4-20250514`
- **THEN** 该模型名维度 MUST 判定为不命中

### Requirement: 上游错误码维度按 JSON 路径取值后精确匹配

上游错误码维度 MUST 先按配置的 JSON 路径从响应正文中取值，再与配置的错误码列表做精确匹配。
取值 MUST NOT 使用子串匹配。

路径列表 MUST 可配置，默认 MUST 为 `error.type`、`error.code`、`code`、`type`。
任一路径取到非空字符串值即 MUST 参与匹配；所有路径均取不到值时，该维度 MUST 判定为不命中。
正文不是合法 JSON 时该维度 MUST 判定为不命中，MUST NOT 使规则保存或判定失败。

默认路径集合 MUST NOT 被当作已逐平台核验的结论；管理员 MUST 可按平台调整路径。

#### Scenario: 按嵌套路径取值

- **WHEN** 路径为默认集合，规则上游错误码为 `["rate_limit_error"]`，
  上游正文为 `{"type":"error","error":{"type":"rate_limit_error","message":"..."}}`
- **THEN** 该维度 MUST 判定为命中

#### Scenario: 取值精确匹配不做子串

- **WHEN** 规则上游错误码为 `["rate_limit"]`，取到的值为 `rate_limit_error`
- **THEN** 该维度 MUST 判定为不命中

#### Scenario: 正文非 JSON 时不命中且不报错

- **WHEN** 上游正文为一段 HTML
- **THEN** 上游错误码维度 MUST 判定为不命中
- **THEN** 本次判定 MUST NOT 失败

### Requirement: 规则的四种动作互斥单选

规则 MUST 恰好携带一个动作，取值为以下四者之一：

- `same_account_retry`：在当前账号重试。MUST 为默认动作，语义 MUST 等同于本变更引入前的行为。
  MAY 附带 0–50 的重试次数覆盖值与 0–60000ms 的固定重试间隔；未提供时 MUST 回落到账号既有
  `pool_mode_retry_count` 与既有退避行为。
- `switch_account`：MUST NOT 在当前账号重试，直接切换账号。
- `terminate`：MUST NOT 重试、MUST NOT 切换账号，直接向客户端返回错误。
  MAY 附带自定义状态码与自定义消息；未提供时 MUST 透传上游状态码与正文。
- `observe`：MUST 记录命中，MUST NOT 改变任何行为。

新建规则时未指定动作 MUST 归一为 `same_account_retry`。

#### Scenario: 重试次数覆盖值缺省时回落账号配置

- **WHEN** 一条 `same_account_retry` 规则命中，且未配置重试次数覆盖值，账号 `pool_mode_retry_count` 为 5
- **THEN** 同账号重试上限 MUST 为 5

#### Scenario: terminate 缺省时透传上游

- **WHEN** 一条 `terminate` 规则命中，且未配置自定义状态码与消息，上游返回 429 与某正文
- **THEN** 客户端 MUST 收到状态码 429
- **THEN** 客户端收到的正文 MUST 为上游正文（经既有脱敏后）

### Requirement: 规则校验拒绝无条件规则与非法取值

创建与更新规则时，系统 MUST 拒绝以下情形并返回可读的字段级错误：

- 所有上游错误条件与空事件条件全部为空
- 名称为空
- 匹配模式不是 `any` 或 `all`
- 动作不在四个允许取值内
- 状态码超出 100–599
- 模型名中的 `*` 不在末尾，或模型名为单独一个 `*`
- 重试次数覆盖值为负，或超过 50
- 重试间隔为负，或超过 60000ms
- 动作为 `same_account_retry` 之外时提供了重试次数覆盖值
- 动作为 `terminate` 之外时提供了自定义状态码或自定义消息
- 自定义状态码超出 100–599

校验 MUST 在服务端执行，MUST NOT 只依赖前端表单校验。

#### Scenario: 拒绝四维全空的规则

- **WHEN** 管理员提交一条四个条件维度均为空的规则
- **THEN** 系统 MUST 拒绝保存
- **THEN** 系统 MUST 返回指明「至少需要一个条件」的字段级错误

#### Scenario: 拒绝非法匹配模式

- **WHEN** 管理员提交匹配模式为 `anyOf` 的规则
- **THEN** 系统 MUST 拒绝保存并指明匹配模式只接受 `any` 或 `all`

#### Scenario: 拒绝非末尾通配的模型名

- **WHEN** 管理员提交模型名 `["*-opus-4"]` 或 `["claude-*-4"]`
- **THEN** 系统 MUST 拒绝保存
- **THEN** 错误 MUST 指明通配符只允许出现在末尾

#### Scenario: 拒绝越界的重试次数覆盖值

- **WHEN** 管理员提交重试次数覆盖值 `-1`
- **THEN** 系统 MUST 拒绝保存
- **WHEN** 管理员提交重试次数覆盖值 `51`
- **THEN** 系统 MUST 拒绝保存并指明上限为 50
- **WHEN** 管理员提交重试次数覆盖值 `0`
- **THEN** 系统 MUST 接受保存

#### Scenario: 拒绝越界的重试间隔

- **WHEN** 管理员提交重试间隔 `60001ms`
- **THEN** 系统 MUST 拒绝保存并指明上限为 `60000ms`

#### Scenario: 混合输入不误判为空请求

- **WHEN** 请求同时包含 `messages: []` 与具有有效文本的 `input`
- **THEN** `request_empty_structured` MUST 不命中

#### Scenario: 无有效输出的流中断

- **WHEN** 上游建立 SSE 后只发送角色、usage、心跳或 reasoning/thinking 事件，随后未正常终止即断流
- **THEN** 系统 MUST 归类为 `empty_interrupted_stream`

#### Scenario: 拒绝与动作不匹配的附加字段

- **WHEN** 管理员提交一条动作为 `switch_account` 但携带重试次数覆盖值的规则
- **THEN** 系统 MUST 拒绝保存
- **WHEN** 管理员提交一条动作为 `observe` 但携带自定义状态码的规则
- **THEN** 系统 MUST 拒绝保存

### Requirement: 预设可建多套，规则归属于预设

系统 MUST 支持创建多个具名预设，每条规则 MUST 归属于恰好一个预设。
预设 MUST 可独立启停。预设内规则 MUST 按优先级数字升序判定，数字越小越先判定；
命中第一条即 MUST 停止判定其余规则。停用的规则 MUST NOT 参与判定。

删除预设时，系统 MUST 一并删除其下规则；若该预设仍被任何账号、分组或全局默认引用，
系统 MUST 拒绝删除并指明引用来源。

#### Scenario: 拒绝删除仍被引用的预设

- **WHEN** 管理员删除一个正被某分组引用的预设
- **THEN** 系统 MUST 拒绝删除
- **THEN** 错误信息 MUST 指明引用来源的类型与标识

#### Scenario: 同一预设内按优先级判定

- **WHEN** 同一预设内两条规则都能命中同一次错误，优先级分别为 10 与 20
- **THEN** 系统 MUST 采用优先级为 10 的规则的动作
- **THEN** 系统 MUST NOT 继续判定其余规则

#### Scenario: 停用的规则不参与判定

- **WHEN** 优先级最高的规则处于停用状态，次高优先级规则也能命中
- **THEN** 系统 MUST 采用次高优先级规则的动作

### Requirement: 三级引用，账号优先于分组，分组优先于全局

预设 MUST 可被账号级、分组级与全局默认三处引用。生效预设的取值顺序 MUST 为
账号 → 分组 → 全局默认，取到第一个非空引用即采用，MUST NOT 跨级合并规则、
MUST NOT 把多级预设的规则集合并后统一排序。

账号级引用 MUST 存放于账号既有的凭据字段容器内，MUST NOT 为此新增账号表列。
被引用的预设处于停用状态或已不存在时，MUST 视为该级未配置并继续向下一级取值。

#### Scenario: 账号级覆盖分组级

- **WHEN** 账号引用预设 A，其所在分组引用预设 B，全局默认引用预设 C
- **THEN** 系统 MUST 只使用预设 A 的规则
- **THEN** 系统 MUST NOT 判定预设 B 与预设 C 中的任何规则

#### Scenario: 不跨级合并

- **WHEN** 账号引用的预设 A 中没有任何规则能命中本次错误，而分组引用的预设 B 中有规则能命中
- **THEN** 系统 MUST 判定为未命中任何规则
- **THEN** 系统 MUST NOT 继续判定预设 B 的规则

#### Scenario: 分组级覆盖全局默认

- **WHEN** 账号未引用预设，分组引用启用中的预设 B，全局默认引用预设 C
- **THEN** 系统 MUST 只使用预设 B 的规则

#### Scenario: 引用的预设被停用时继续向下取值

- **WHEN** 账号引用的预设 A 处于停用状态，分组引用启用中的预设 B
- **THEN** 系统 MUST 使用预设 B 的规则

#### Scenario: 三级均未配置

- **WHEN** 账号、分组与全局默认均未引用任何预设
- **THEN** 系统 MUST 回落到账号既有 `pool_mode_retry_status_codes` 与 `pool_mode_retry_count` 的现状行为

### Requirement: 生效来源必须可在页面上看到

管理端 MUST 能对给定账号展示当前生效的预设及其来源级别（账号／分组／全局默认／未配置）。
MUST NOT 只展示引用值而不展示实际生效结果。

#### Scenario: 展示生效来源

- **WHEN** 管理员查看一个自身未配置、但所属分组已配置预设的账号
- **THEN** 页面 MUST 展示生效预设的名称
- **THEN** 页面 MUST 标明该预设来自分组级

#### Scenario: 引用失效时展示回落结果

- **WHEN** 账号引用的预设已被停用，分组与全局默认均未配置
- **THEN** 页面 MUST 展示生效结果为「未配置」
- **THEN** 页面 MUST NOT 把已停用的预设展示为生效中

### Requirement: 页面必须明示生效范围与已知限制

管理端页面 MUST 列出当前生效的链路范围与作用范围闸门，并 MUST 明示以下已知限制：

- WebSocket 链路只在上游状态码为 429 且同账号重试截止时间非零时才进入同账号重试判定，
  规则 MUST NOT 放宽该前置
- OpenAI Alpha Search 与 OpenAI Images 两条链路的账号惩罚抑制不完整，
  `switch_account` 在这两条链路上仍可能向熔断器上报一次失败

页面 MUST NOT 让管理员误以为所有动作在所有链路上语义完全一致。

#### Scenario: 页面标注链路限制

- **WHEN** 管理员查看重试过滤页面
- **THEN** 页面 MUST 列出当前生效的链路范围
- **THEN** 页面 MUST 明示 WebSocket 链路的 429 前置限制
- **THEN** 页面 MUST 明示上述两条链路的惩罚抑制不完整

### Requirement: 规则缓存与多实例一致性

系统 MUST 为规则判定提供内存缓存，并 MUST 在缓存项中预先计算小写关键词、小写模型名前缀与
状态码集合，MUST NOT 在每次上游错误的判定路径上重复做大小写转换或线性扫描列表。
正文的小写化 MUST 延迟到确实需要关键词匹配时才进行。

系统 MUST 复用既有错误透传规则的缓存形状：Redis 共享缓存叠加本实例内存缓存，
并通过发布订阅通道做跨实例失效。任一预设或规则发生增删改、或三级引用发生变更时，
系统 MUST 使本实例缓存失效，并 MUST 通知其余实例失效。
通知失败时 MUST 记录日志并降级，MUST NOT 中断管理员的保存操作。

#### Scenario: 保存后本实例立即生效

- **WHEN** 管理员保存一条规则
- **THEN** 同一实例的后续判定 MUST 使用新规则
- **THEN** 系统 MUST NOT 要求重启进程

#### Scenario: 跨实例失效

- **WHEN** 管理员在实例 A 上修改某预设的规则
- **THEN** 实例 B MUST 收到失效通知并在后续判定中使用新规则
- **THEN** 实例 B MUST NOT 继续使用失效前的缓存项

#### Scenario: 引用变更同样触发失效

- **WHEN** 管理员改变某分组引用的预设
- **THEN** 各实例的生效预设解析结果 MUST 随之更新

#### Scenario: 跨实例失效通知失败时降级

- **WHEN** 管理员保存规则，而跨实例通知通道不可用
- **THEN** 保存 MUST 成功
- **THEN** 系统 MUST 记录通知失败日志

### Requirement: 仅管理员可见可配，且不含凭据字段

重试过滤的预设与规则 CRUD、三级引用的读写、生效来源查询 MUST 全部挂在管理端路由前缀
`/api/v1/admin/*` 下，MUST NOT 提供任何 `/api/v1/my/*` 或其他普通用户作用域的入口。

预设与规则的数据模型 MUST NOT 包含令牌、密码、代理认证、上游 URL、订阅地址
或任何可还原凭据的字段。管理端响应体 MUST NOT 携带此类字段，即便其来源对象含有。

#### Scenario: 普通用户作用域无该能力

- **WHEN** 检索普通用户作用域的路由注册
- **THEN** MUST 不存在任何重试过滤相关路由

#### Scenario: 普通用户调用管理端接口被拒

- **WHEN** 已登录的普通用户直接请求重试过滤的管理端接口
- **THEN** 系统 MUST 拒绝该请求
- **THEN** 响应 MUST NOT 包含任何预设或规则内容

#### Scenario: 模型不含凭据字段

- **WHEN** 检查预设与规则的数据模型及其序列化输出
- **THEN** MUST 不存在令牌、密码、代理认证、上游 URL 或订阅地址字段

#### Scenario: 生效来源响应不泄露账号凭据

- **WHEN** 管理员查询某账号当前生效的预设与来源级别
- **THEN** 响应 MUST 只包含预设标识、名称与来源级别
- **THEN** 响应 MUST NOT 包含该账号凭据容器内的其他字段
