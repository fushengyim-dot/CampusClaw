# 第一轮技术方案

## Context

当前项目只有说明和规约。用户已确定以本门软件开发课程的网页、课件与实操教程作为资料，保留教师/学生身份以及三个后续 AI 应用。第一轮只实现 `proposal.md` 定义的基础能力，验收标准见 `specs/auth-upload/spec.md`。

## Goals / Non-Goals

目标是让教师与学生登录、限制本班访问、可靠保存讲义与正文，并在本地通过 Compose 重复启动。非目标完整沿用 proposal，不把“知识库入库”称为“已支持检索或问答”。

## Decisions

### D01：React/TypeScript + Go + MySQL，同一仓库

前端展示登录、讲义列表、详情和上传表单；Go 执行业务规则；MySQL 保存数据。Nginx 提供静态页面并转发 `/api/` 和 `/health`。这是已讨论的技术方案，采用第 3 课教程的 Go/MySQL 方向；第 2 课 Flask/SQLite 是等价示例，不直接复制它的实现路径。

依赖方向为 `handlers → services → repositories`。`middleware` 在业务处理之前取得可信身份；`database` 管理连接与建表，`config` 读取配置。不得把全部逻辑放进 `main.go`。

第 3 课按需创建：

```text
frontend/src/pages/         登录、讲义列表、讲义详情
frontend/src/api/           统一发送登录和材料请求
backend/main.go             启动和登记接口
backend/handlers/           接收请求、校验格式、返回响应
backend/middleware/         登录检查、来源检查
backend/services/           登录、材料上传等业务步骤
backend/repositories/       按班级查询和事务写入
backend/database/           连接、migrations、样例初始化
backend/models/             本轮数据结构及未来实体占位
backend/config/             环境变量读取
deploy/nginx/               同源入口配置
```

### D02：服务端会话，Cookie 只携带随机标识

登录成功后生成至少 32 随机字节的不可预测会话标识。浏览器仅保存 `campusclaw_session` Cookie，不保存角色或班级；服务端 `sessions` 记录关联用户，鉴权时读取 `users` 的角色和班级。用环境变量 `SESSION_SECRET` 对标识进行 HMAC-SHA-256 后保存摘要，不在数据库或日志保存原始标识。

成功登录必须作废本次请求携带的旧会话并换发新标识；退出删除当前会话记录、清 Cookie，重放旧 Cookie 返回 401。会话采用固定 8 小时有效期，不自动续期。8 小时和 32 字节是本项目初始选择，不是教师指定数值。

Cookie 设置 HttpOnly、SameSite=Lax、Path=/，不设置跨站 Domain；本地 HTTP 演示 `Secure=false`，启用 HTTPS 时必须为 true。写请求（登录、退出、上传）必须验证 `Origin` 与配置 `APP_ORIGIN` 完全一致；缺失或不符返回 403。检查脚本须显式携带该 Origin。SameSite 不作为唯一来源校验。

第一轮不做 JWT；第 5 课 Bearer 令牌接入通过新变更调整，保持班级授权规则不变。

### D03：密码和配置

密码使用成熟库提供的 bcrypt 哈希和验证，禁止自写加密或明文存储；初始 cost=12。初始化密码来自环境变量，缺少或空值时首次初始化失败，不使用固定默认口令；bcrypt 输入按字节限制不超过 72 字节，不静默截断。

计划配置变量：`APP_ORIGIN`、`WEB_PORT`、`SESSION_SECRET`、`MYSQL_HOST`、`MYSQL_PORT`、`MYSQL_DATABASE`、`MYSQL_USER`、`MYSQL_PASSWORD`、`MYSQL_ROOT_PASSWORD`、`SEED_TEACHER_A_PASSWORD`、`SEED_STUDENT_A_PASSWORD`、`SEED_TEACHER_B_PASSWORD`、`SEED_STUDENT_B_PASSWORD`。`.env.example` 只列变量名和说明，无真实值。

`SESSION_SECRET`、数据库必需配置缺失时拒绝启动；样例密码仅在首次初始化时需要，后续启动不得用环境变量重新覆盖既有密码。密钥、口令和原始会话标识不写日志、不放入响应JSON或前端脚本；会话标识仅通过HttpOnly Cookie下发。上传正文不写日志，但经过授权的材料详情必须返回正文。

### D04：数据结构与样例

| 表 | 第一轮用途及关键字段 |
| --- | --- |
| `classes` | 班级：id、name |
| `users` | 账号：id、username（唯一）、password_hash、role、class_id |
| `sessions` | 会话：token_hash、user_id、expires_at |
| `materials` | 材料：id、class_id、uploaded_by、title、original_filename、storage_key、size_bytes、sha256、source_url（可空）、source_locator（可空）、created_at |
| `knowledge_entries` | 正文：id、material_id（唯一）、class_id、content |
| `assignments` | 最小占位：id、class_id、title；无业务接口和页面 |
| `tutor_agents` | 最小占位：id、class_id、name；无业务接口和页面 |
| `skills` | 最小占位：id、class_id、name；无业务接口和页面 |

前五张是第 3 课实际数据链路；后三张为第 2 课六类核心实体要求保留空结构。六类对应 classes、users、materials（讲义）、assignments、tutor_agents、skills；sessions 和 knowledge_entries 是基础支持表。本轮不向后三表写业务样例，也不实现相关功能。

初始化至少准备 A/B 两班、`teacher_a`、`student_a1`、`teacher_b`、`student_b1` 四个账号；两班各一份有明显班级标记的虚构软件开发课程验收材料，原文件、materials 和 knowledge_entries 均对应。虚构样例只用于权限检查，不冒充老师原文。真实课程资料转换另有任务。

初始化必须幂等：第二次执行不复制账号和材料，不重置密码，不清空用户上传。材料与正文的班级一致性由同一服务端身份写入，并用 `(material_id, class_id)` 外键关联到 materials 的对应唯一键。

### D05：每一次数据访问都过滤班级

身份来自会话。请求中自行提供的 role、class_id 一律不作为授权依据，class_id 参数按第 3 课教程丢弃。列表使用会话 class_id，详情/下载使用 `id AND class_id`，知识库正文访问同时约束材料归属和正文 class_id。

跨班 ID 与不存在 ID 均为 404 和同一 JSON 消息，不泄露标题、正文、大小、存储路径。不得先读取跨班材料返回部分字段再补检查。

教师与学生只在“能否上传”上有第一轮差异，教师也不能查看其他班。学生上传在文件落盘或数据库写入前返回 403。前端隐藏上传按钮仅用于界面体验。

### D06：接口与页面约定

| 接口 | 身份 | 成功结果 |
| --- | --- | --- |
| `POST /api/login` | 无需已有会话，但检查 Origin | 200，设置新会话 Cookie，返回用户非敏感信息 |
| `POST /api/logout` | 有效会话，检查 Origin | 204，删除会话并清 Cookie |
| `GET /api/me` | 已登录 | 200，当前 id、username、role、class_id |
| `GET /api/materials` | 本班教师/学生 | 200，材料列表；空班级为 `items: []` |
| `POST /api/materials` | 本班教师，检查 Origin | 201，新材料 id/title |
| `GET /api/materials/{id}` | 本班教师/学生 | 200，材料标题、正文、来源等安全字段 |
| `GET /api/materials/{id}/download` | 本班教师/学生 | 200，原文件附件 |
| `GET /health` | 无需登录 | 就绪 200；依赖不可用 503，不泄露内部连接信息 |

API 错误统一为 `{"error":{"code":"...","message":"..."}}`。未登录/过期为 401，已登录但非教师上传为 403；跨班与不存在为 404；无效输入为 400，文件过大为 413，文件类型不支持为 415；内部保存失败为 500（对外通用说明，不返回 SQL、路径、堆栈）。401 登录失败消息统一“用户名或密码错误”，不区分账号不存在。

前端页面 `/login`、`/materials`、`/materials/{id}`。未登录打开后两种页面时不显示材料，路由检查引导到 `/login`；底层 API 必须独立返回 401。允许静态页面壳返回 200，不将前端跳转冒充 HTTP 302。登录成功进入讲义列表；登出回到登录页。页面提供加载、空列表、失败状态。

列表第一页 page=1、page_size=20；page_size 最大100，非法参数返回400。按 created_at 倒序、id 倒序排列。以上分页数值为项目默认值，不是教师给定。

### D07：文本上传、正文入库与回滚

上传表单含 title（去首尾空白后1—200字符）、file；可选 source_url（仅 http/https，最长2048字符，不由服务器访问）、source_locator（最长200字符）。标题和来源字段纯文本处理。原文件名仅作为展示信息，不参与文件路径拼接。

只接受不区分扩展名大小写的 `.txt`、`.md`；内容须为合法 UTF-8（可带 BOM），解码去空白后非空且不含 NUL。单文件上限 5 MiB（5,242,880 字节），整个 multipart 请求上限 6 MiB（6,291,456 字节）。这些为项目初始参数；Nginx 和 Go 都实施限制，Go 仍负责文件精确上限。超限返回413，不支持格式415，其余校验失败400。

处理顺序：

1. 检查登录、Origin、教师角色；取得可信 class_id。
2. 校验表单和文件；内容最多读取到上限加1字节即判超限，不无界占用内存。
3. Go 生成随机存储名，写入上传卷中的本班目录，不采用上传文件名作为路径，不覆盖已有材料。
4. 开启 MySQL 事务，写 materials 和 knowledge_entries，两者 class_id 完全相同；提交后才返回201。
5. 任一正常失败路径回滚两张表并删除本次新增文件。成功后刷新列表、查看和下载都从实际持久化数据读取。

磁盘文件与 MySQL 不是同一种事务资源：数据库回滚不会自动删除文件。必须实现文件清理补偿；若进程在写文件后崩溃，重启时在接收上传前扫描未被数据库引用的临时/孤立文件并清理，无法清理则保持不就绪并记录不含敏感信息的错误。只处理本应用数据卷中确认未引用的文件，不触碰其他路径。不能宣称已实现跨文件系统与数据库的分布式原子事务。

每次成功上传生成新材料，即使原文件名相同也不覆盖。Markdown 首版按纯文本安全显示，禁止直接执行上传 HTML/脚本。下载使用 attachment，不暴露服务器路径。文件上传保存仍不代表已切片或已可检索。

### D08：来源信息与课程资料准备

转换得到的 `.md` 推荐保留课次、章节、原始URL和原始位置；上传时保存可提供的 source_url/source_locator。没有真实页码时不编造页码，没有原地址时留空。第一轮只保存和显示来源，答案引用与检索在第 4—5 课实现。

PPT/PDF 中的代码、命令、表格或图片解释需要在转换任务中核对。真实转换未完成前，只用明确标注的虚构样例验收上传链路，不声称课程知识库已经建立完整。

### D09：Compose、入口与数据保留

第 3 课提供 `compose.yaml`，固定项目名 `campusclaw-course`，服务 web（Nginx与构建后的前端）、api（Go）、db（MySQL）。本地仅 web 发布 `127.0.0.1:${WEB_PORT}`，端口实施前检查占用。api、db 仅内部网络可达，不发布宿主机端口。

使用命名卷 `mysql_data` 和 `uploads_data`。上传卷仅挂载给 api，Nginx 不挂载、不公开 uploads。初始化配置与种子任务完成后 api 才标记就绪；`/health` 需要检查数据库可用、数据结构已准备和上传目录可用。

普通 `docker compose down` 再 `up` 不删除上述数据卷，且沿用同一项目名与卷时，账号、讲义原文件和正文都应保留。`down -v`、手动删除数据卷、切换项目名产生新卷不属于此保证。验证只在本项目独立环境进行。

`.env` 不提交也不 COPY 进镜像；`.dockerignore` 排除私密配置、运行数据、缓存和检查报告。第 3 课 README 写出从配置到启动、登录、health、普通重启的真实可执行方法。

## Risks / Trade-offs

- 上传文件与数据库需补偿清理；只看返回码不能证明一致性，必须同时核对数据库和文件。
- 前后端分离比单文件演示多一层配置，采用同源 Nginx 入口统一路径；Cookie 登录的 Origin 检查也在 Go 实施。
- 第 2 课含六类实体占位，第 3 课先用五张活动表；通过空表占位兼容两份材料，不提前实现作业业务。
- 固定账号、单班归属和本机 HTTP 仅适用于当前演示范围；公网发布、账号生命周期和 HTTPS 必须在后续已授权变更里设计和验收。

## Migration Plan

这是新建仓库，不迁移旧系统。用户确认规约后，第 3 课按 tasks 顺序建立源码、数据结构和样例；每步验证后记录证据。全部场景通过后才归档当前变更并更新长期规格。

## Open Questions

无阻断本轮规约审阅的问题。第 3 课开始时需核对本机 Docker 可用性、入口端口和依赖版本；它们是实施环境检查，不改变本轮行为要求。会话时长、上传上限等项目选择可在用户审阅时调整，调整须同步 spec 和 tasks。
