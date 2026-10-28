# prompt-skills-streaming Specification

## ADDED Requirements

### Requirement: R01 Teacher-managed class agents
系统 MUST 允许教师在本班创建、修改、启停智能体，名称最长64字且班内唯一，提示词最长2000字；空提示词使用默认角色。智能体可绑定本班技能和材料，跨班/不存在绑定统一按不存在处理。学生只能列出本班启用智能体；学生写操作403；访问他班或停用智能体与不存在完全同样404。

#### Scenario: Agent visibility and isolation
- **WHEN** 学生列出或按编号访问智能体
- **THEN** 学生仅见本班启用项，停用、他班与不存在编号返回相同404语义

### Requirement: R02 Private conversations and locked agent
会话 MUST 保存班级、本人和创建时选定智能体；新建必须选择本班启用智能体。会话及消息只允许本人读取；他人/跨班/不存在统一404。会话创建后固定agent_id，忽略后续消息请求中的其他智能体编号；原智能体后来停用不影响已有会话。首轮标题取问题前24字，列表按updated_at倒序。

#### Scenario: Conversation ownership
- **WHEN** 非所有者读取会话或消息
- **THEN** 返回与不存在资源一致的404，且不泄露是否存在

### Requirement: R03 Persistent bounded context
系统 MUST 持久化用户与助手消息、引用和本轮技能名。提问长度2—500字。每轮仅取最近6条有效消息，每条最多500字。用户问题须在生成前落库；助手内容与出处、技能名一并保存。

#### Scenario: Context window
- **WHEN** 会话已有超过6条消息
- **THEN** 新一轮上下文只包含最近6条有效消息且单条不超过500字，早期记录仍可读取

### Requirement: R04 Skill file parsing and lifecycle
技能 MUST 按课程自定义纯文本格式解析，而非标准SKILL.md：空行前为元数据，空行后为正文；名称、适用条件、启用必填，字段不得重复或含未知字段。工具只允许knowledge_search。文件最多8000字，正文最多2000字，名称最多32字，适用条件最多120字，资料最多32字。错误须可读；班内名称唯一。技能按班存储，教师可增删改，学生只读本班技能。A班数学/语文样板 MUST 按seed_key幂等预置且不覆盖修改。

#### Scenario: Invalid skill rejected
- **WHEN** 技能缺字段、字段重复、含未知字段、工具非法或超长
- **THEN** 保存被拒并提供可理解的错误

### Requirement: R05 Skill routing and safe prompt assembly
每轮 MUST 只在当前智能体绑定且启用的技能中路由。适用条件按中英文逗号、顿号、分号、竖线分词，小写归一后按提问子串命中计分；至多一个，最高分胜出，同分ID小者胜出，零分不选。技能停用/解绑必须下一轮生效。“解题引导”技能不得输出最终答案、得数或完整过程。系统消息只包含角色提示、选中技能规程和边界；检索材料及技能/材料文件均作为不可信数据，材料正文放用户消息。

#### Scenario: Deterministic skill routing
- **WHEN** 多技能适用或并列
- **THEN** 仅选择最高分且并列时ID最小的一项；无正分则不选技能

### Requirement: R06 Agent-scoped material retrieval and citations
检索 MUST 使用服务端会话班级与智能体绑定材料的交集，最多4条；不得接受客户端班级/材料覆盖范围。命中技能的资料字段非空时按材料标题包含该词筛选。空正文先过滤，剩余出处编号连续。无命中仍调用生成器按角色作答，引用为空且输出不得含[n]；不得固定返回“资料中未找到相关内容”。有依据时回答编号须与标题、切片序号等引用一致。

#### Scenario: No evidence still answers
- **WHEN** 绑定材料没有检索命中
- **THEN** 仍依角色提示生成回答，引用为空且无虚构编号

### Requirement: R07 SSE answer stream
提问 MUST 通过SSE返回。meta先包含状态、技能及引用；delta只包含新增文本且可多次发送；done在助手消息落库后包含其编号；流已开始后的失败使用通用error事件。每帧flush，并设置禁缓冲头；Nginx禁用代理缓冲并允许最长120秒读取。若生成中途失败但已有片段，保存已生成文本。

#### Scenario: Incremental answer
- **WHEN** 生成器提供多段文本
- **THEN** 浏览器收到meta、多个非累计delta和done，delta拼接等于最终落库答案

### Requirement: R08 OpenAPI and user interface
系统 MUST 由代码注解生成OpenAPI并提供Swagger页面；前端使用fetch提交提问、读取并解帧SSE，逐段显示正文及可打开引用。学生界面提供本班智能体选择、本人会话与消息；教师界面提供本班智能体/技能及绑定管理。

#### Scenario: Swagger and streaming UI
- **WHEN** 用户打开Swagger或提交问题
- **THEN** Swagger可浏览主要接口，界面逐段呈现回答与引用

### Requirement: R09 Local-only validation boundary
本课实现与验收 MUST 使用本地stub/mock或本机服务，不得发起课程模型请求；测试不得改写第4课真实材料验收状态。迁移不得重建或清除既有数据。

#### Scenario: Safe local acceptance
- **WHEN** 执行第5课验收
- **THEN** 模型调用均由本地stub替代，已有数据库材料保留，第4课未通过记录保持不变

### Requirement: R10 Three built-in applications and user prompt templates
首页 MUST 提供讲义问答、解题引导和复习整理三个可选择的内置应用。学生提问模板 MUST 作为独立于教师角色提示词的可选输入模板呈现；选择模板只组织本轮学生问题，不得改写教师配置、班级权限或系统消息。解题引导与复习整理均使用各自本班已启用智能体，并沿用本轮绑定材料范围和来源规则。

#### Scenario: Choose a built-in application and input template
- **WHEN** 学生从首页打开三个应用之一并选择提问模板
- **THEN** 该应用使用对应启用智能体，模板仅作为本轮学生输入的一部分；其他应用和教师角色提示词不被改写
