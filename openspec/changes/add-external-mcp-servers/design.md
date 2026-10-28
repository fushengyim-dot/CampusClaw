# Design: 外部 HTTPS MCP

## Decisions

1. 保留项目内置 `http://mcp-tools:8091/mcp` 作为固定例外；其余仅接受不带用户名、查询串或片段的 HTTPS URL。
2. 外部服务器必须提交长度合规的专用 Bearer token；不复用内置服务器令牌。更新已登记的相同URL时可保留旧密文；更改URL必须重新提供token。
3. 服务器端禁用环境代理与HTTP重定向。每次拨号时解析DNS，要求所有结果均为公网IPv4，再直接拨到已验证的解析地址，以减少DNS重绑定窗口；不访问公网作为测试步骤。
4. 初始化、`notifications/initialized`、工具发现和工具调用遵循2025-11-25会话与版本头；响应支持application/json和text/event-stream，读取限制256KiB。一次工具调用建立临时MCP会话，不将会话ID写入数据库或审计记录。
5. 页面显示外部HTTPS地址输入框、一次性token输入框和安全边界说明。连接只在教师显式点击时发起，错误消息不回显token或服务端正文。
6. 工具仍需教师逐项绑定；调用参数经服务端JSON Schema二次校验，出站工具调用轮次/时间和结果大小继续受限。

## Risks and limits

- 仅支持MCP 2025-11-25 Streamable HTTP及静态Bearer token；OAuth-only、stdio、旧版HTTP+SSE、服务端主动GET流/恢复重放不支持。
- 拒绝私网/IPv6目的地可能排除企业内网服务和IPv6-only托管服务。这是避免服务端请求伪造的安全限制。
- SSRF拦截降低风险，不替代部署级出口防火墙；公网MCP服务器本身仍可能返回恶意工具描述或结果，现有schema审查和不可信结果约束继续生效。
