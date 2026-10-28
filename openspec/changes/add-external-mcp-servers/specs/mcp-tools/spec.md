## ADDED Requirements

### Requirement: 外部 HTTPS MCP 服务器
系统 MUST 允许教师为本班登记外部 HTTPS Streamable HTTP MCP endpoint 和该服务器专用 Bearer token，并在教师显式执行连接操作后进行协议初始化与工具发现。系统 MUST 支持 MCP `2025-11-25` 初始化/会话/版本头、`notifications/initialized`及JSON和SSE响应；服务器返回会话ID时，后续请求必须携带该ID。工具调用必须为该调用建立并使用独立会话，且会话标识不得持久化或写入审计摘要。

外部endpoint MUST 拒绝非HTTPS、用户信息、查询串、片段、无效主机和私网/回环/链路本地/保留IP。连接时 MUST 将DNS解析得到的公网地址固定到本次拨号，不使用环境代理且不跟随HTTP重定向；不得把课程内置token复用到外部地址。更新外部服务器地址时 MUST 要求新token。令牌 MUST 加密保存且不得返回页面或日志。

系统 MUST 保留发现结果检查、教师对本班技能工具的显式绑定、JSON Schema参数校验、调用轮数/次数/超时、结果大小限制、错误脱敏、班级隔离和审计。测试只能使用本机合成MCP服务器/stub，不得连接真实第三方MCP服务。

该要求不承诺支持OAuth-only认证、stdio进程、旧版HTTP+SSE、主动GET通知流或事件恢复重放；页面及文档 MUST 清楚说明这些范围。

#### Scenario: 教师发现外部工具
- **WHEN** 教师登记合法外部HTTPS endpoint和专用token并点击连接
- **THEN** 系统执行协议握手并显示该本班服务器发现的工具，token不出现在响应或页面中

#### Scenario: 拒绝内部网络目标
- **WHEN** 教师登记私网/回环/链路本地/保留地址，或DNS解析到此类地址
- **THEN** 系统拒绝连接且不得发送MCP请求

#### Scenario: 工具调用携带会话
- **WHEN** 已绑定工具被授权的课程流程调用且MCP服务器分配会话ID
- **THEN** 初始化后的通知和调用请求携带该会话ID，且会话ID不写入持久存储或审计日志

#### Scenario: 兼容格式错误或超限服务器
- **WHEN** 外部服务返回无效协议响应、重定向或超过256KiB的响应
- **THEN** 系统安全失败、清空旧发现清单并展示通用错误
