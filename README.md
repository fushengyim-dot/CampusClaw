# CampusClaw 课程学习平台

> 2026-10-28晚复验：个人项目整体尚未通过。课堂材料已保存绑定，绑定范围检索与班级隔离检查通过；工具状态显示和正文流式已修复并部署，本地分段与中断测试通过。一次无材料/技能/工具的真实普通流式与保存回读已通过；真实工具调用、技能上传完整页面、截图和同伴检查仍待验；另有新建会话返回失败但已保存的异常待查。

CampusClaw 是本门软件开发课程的本地学习平台。教师和学生使用各自账号登录；页面只显示本班讲义。教师可上传 UTF-8 的 TXT 或 Markdown 文件，班级成员可查看正文并下载原文件。

公开阅读入口：[开发与验收流程](docs/PROJECT_HISTORY.md) · [五题报告PDF](docs/report/CampusClaw_Report_v3.pdf)。Git历史保留实际提交，不包含Codex对话。

## 当前实现

- 登录、退出和服务端会话已接通；会话固定 8 小时，角色与班级由服务端账号记录决定。
- 注册入口已加入：保留已有预置账号；教师登录后可为本班签发24小时有效的一次性教师/学生邀请码，注册人不能选角色或班级。邀请码只在签发时显示，数据库只保存摘要；教师可查看状态并撤销未使用邀请码。新账号使用现有HttpOnly服务端会话登录。
- A、B 两班各有一位教师和一位学生。学生只能查看和下载本班材料。
- 教师可上传 `.txt`、`.md` 文件；单个文件最多 5 MiB，上传请求最多 6 MiB。
- 数据库同时保存讲义信息和正文；原文件存放在本项目的 F 盘目录。程序启动时校验文件与数据库一致，并补入缺少的课程验收样例。重复启动不会复制样例或重置已有密码/讲义。
- 上传、正文详情和下载都经过服务端权限检查。第4课已实现关键词、向量和混合检索、讲义出处、教师手动向量化入口，以及有检索依据才回答的问答页面。讲义不会在上传时自动发送；教师每次手动启动最多处理10段。问答会把问题发送到课程嵌入服务，找到本班资料后还会把问题和最多4段资料发送到课程对话服务。
- 第5课本机实现已完成：页面提供讲义问答、解题引导、复习整理三个入口；教师可维护本班智能体和样板技能，也可以选择课程自定义格式的UTF-8 `.txt`/`.md` 技能文件，先检查文本再确认保存。该上传入口不表示兼容标准 `SKILL.md` 目录。会话流式接口、OpenAPI与Swagger已经接入。Go离线stub和前端测试检查了分段事件、无资料时不显示伪造引用、资料筛选、中途失败保存已生成内容、UTF-8拆帧及技能文本文件读取校验；没有发送课程模型请求。学生真实浏览器提问、逐段显示和同伴交叉检查仍待完成。
- 第6课本机实现已完成：独立MCP服务提供两个只读课程工具；教师可在“工具接入”页连接内置服务或登记外部HTTPS MCP服务器、发现工具、管理技能绑定并查看安全摘要调用记录。外部接入限定为MCP `2025-11-25` Streamable HTTP和静态Bearer token；不支持OAuth-only、stdio、旧版HTTP+SSE或IPv6/私网目标。系统对外部地址禁用代理和跳转，校验并固定DNS公网IPv4；token在数据库中加密，且只在教师显式连接时发起请求。工具参数在服务端再次检查，调用轮数、次数、时限和结果大小均有限制。Go/前端测试、Windows/Linux构建、Compose配置和规约严格校验通过；外部服务只用本机合成stub测试，没有连接真实第三方服务器或课程模型。教师页面修改后截图和真实外部兼容性仍待验收。第6课记录见 [`docs/lesson-05-06/internal/ACCEPTANCE-06.md`](docs/lesson-05-06/internal/ACCEPTANCE-06.md)。第5课学生浏览器/同伴检查、第6课同伴检查和课程模型工具调用、第4课材料范围失败记录仍保留。

## 本机启动

需要 Docker Desktop、PowerShell、Node.js/npm，以及本项目固定使用的 Go 工具。数据库和上传文件都留在本项目 F 盘 `runtime/`；不要移动或删除这些目录。Docker 镜像沿用电脑现有 Docker 存储。

在项目根目录运行：

```powershell
./scripts/database.ps1 -Task prepare
./scripts/database.ps1 -Task prepare-accounts
./scripts/database.ps1 -Task up
```

第4课课程网关配置使用项目根目录的 `.env`：新克隆项目时先复制 `.env.example` 为 `.env`，再填写课程页面提供的地址、密钥、模型名和维度。本机已有 `.env` 骨架时，在 Trae 中打开它并把密钥粘到 `EMBEDDING_API_KEY=` 后面。`.env` 已被 Git 忽略，且不会打进镜像。Docker Compose 会从项目根目录读取它，并只把模型配置传给 API 容器；Go 程序本身不解析 `.env`。密钥不要写进源码、普通文档或聊天。`test-embedding` 每次运行会向课程网关发送一条自拟短句；`test-chat` 每次运行会发送一条自拟问题和一段合成资料。两种命令都是真实模型请求，可能计入额度，不会自动重试；再次运行前应先考虑额度。

首次启动前两条命令会创建缺少的本地秘密文件和四份随机演示账号密码；重复执行不会覆盖现有文件。密码保存在 `runtime/private/`，不应复制到聊天、日志或普通文档。`up` 会构建前后端并启动本项目的数据库、API 和网页；只有网页发布到本机 `127.0.0.1:8080`，数据库与 API 不对宿主机开放端口。

浏览器访问 [http://127.0.0.1:8080](http://127.0.0.1:8080)。账号名为 `teacher_a`、`student_a1`、`teacher_b`、`student_b1`；对应密码由首次准备时写入本地秘密目录。健康检查地址为 [http://127.0.0.1:8080/health](http://127.0.0.1:8080/health)，服务完全就绪返回 `{"status":"ok"}`。

停止服务但保留数据：

```powershell
./scripts/database.ps1 -Task stop
```

再次执行 `up` 会用同一份数据库和上传文件。普通停止或容器重建不会清空这些数据；不要用删除卷/数据目录的命令来“重置”。

## 检查命令

```powershell
./scripts/dev.ps1 -Task test
./scripts/dev.ps1 -Task build
./scripts/dev.ps1 -Task build-linux
./scripts/database.ps1 -Task test-materials
./scripts/database.ps1 -Task test-chat
openspec validate add-auth-rbac-class-knowledge --strict --no-interactive
```

`test-chat` 是一次性的真实网关连通检查，不是离线单元测试；只有确认可以消耗额度时才运行。它不会显示模型回答。

`test-materials` 会在当前项目数据库中建立带专用前缀的临时测试班级和材料，验证事务、5 MiB 边界、未知提交与恢复；结束时只清理由该测试创建的记录和文件。运行证据和当前限制见 [`docs/lesson-03/internal/MATERIALS.md`](docs/lesson-03/internal/MATERIALS.md)。

## 课程资料与范围

虚构样例明确标注为验收用途。课程范围整理稿根据[课程第3课公开教程](https://devops.hello1023.com/课件/第3课-课件-认证授权与知识库入库/pages/scope.html)核对，并保留来源网址与章节位置。第4课已实现三类切片策略、MySQL中文关键词检索、Qdrant向量检索、RRF混合排序、向量状态与正文变化检查、来源引用和证据门控问答。合成向量的MySQL/Qdrant隔离集成检查通过。课程网关短句连接和合成对话都通过；另有一次经授权的真实材料问答尝试，但对话请求实际带了3条检索资料，而授权范围只有1条，因此该真实闭环验收失败。具体记录见`docs/lesson-04/internal/GATEWAY_CONFIG.md`；目前没有追加请求。向量/混合搜索和问答会向课程服务发送问题或讲义内容，可能消耗额度。老师指向的第4课原始OpenSpec文件尚未取得，整课也未最终验收。

## 目录入口

- [`frontend/src/App.tsx`](frontend/src/App.tsx)：登录、材料列表、详情、上传、三种检索方式、手动向量化和出处问答页面。
- [`backend/handlers/application.go`](backend/handlers/application.go)：HTTP 接口和就绪状态优先检查。
- [`backend/services/materials.go`](backend/services/materials.go)：班级查询、上传事务、文件校验和恢复。
- [`backend/services/chunking.go`](backend/services/chunking.go)：自动、自定义和 Markdown 标题分块。
- [`backend/services/knowledge.go`](backend/services/knowledge.go)：切片补建、关键词/向量/混合检索及手动向量索引状态。
- [`backend/services/chat.go`](backend/services/chat.go)：服务端课程对话网关；真实问答只会在用户提交问题且找到本班资料后触发。
- [`backend/handlers/lesson5.go`](backend/handlers/lesson5.go)：教师/学生智能体与技能接口、本人会话权限和SSE流式应答。
- [`backend/handlers/openapi.go`](backend/handlers/openapi.go)：接口说明JSON及Swagger页面。
- [`backend/mcpserver/server.go`](backend/mcpserver/server.go)：独立MCP HTTP服务、认证、健康检查及本地只读课程工具。
- [`backend/services/mcp_orchestrator.go`](backend/services/mcp_orchestrator.go)：仅使用技能当前绑定工具的受限调用循环；自动检查使用本地stub，不触发外部模型请求。
- [`compose.yaml`](compose.yaml)：本项目 web/api/db 服务、网络与 F 盘挂载。
- [`openspec/changes/add-auth-rbac-class-knowledge/tasks.md`](openspec/changes/add-auth-rbac-class-knowledge/tasks.md)：第3课要求和逐项验收状态。
- [`TASK_BOARD.md`](../TASK_BOARD.md)：整个课程项目的总任务状态。
