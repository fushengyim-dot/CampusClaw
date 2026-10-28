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

第3课配置检查增量：后端从进程环境读取APP_ORIGIN、WEB_PORT、SESSION_SECRET、MYSQL_HOST/PORT/DATABASE/USER/PASSWORD；必填项空白视为缺失，密码原值不裁剪。APP_ORIGIN仅允许http/https的来源（无用户信息、路径、查询或片段）；端口须为1—65535的十进制整数。新增仅用于本机开发的API_PORT，可选默认18080，监听地址固定127.0.0.1；Compose内部监听在部署步骤另行配置，不将开发监听当作S30部署完成。MYSQL_ROOT_PASSWORD供未来容器使用，种子密码供未来首次初始化使用，当前后端不读取它们。

`--check-config`只验证格式并退出，不打开端口、不连接数据库；错误仅包含固定变量名和说明。后续新增--check-db真实连接检查；正常启动也验证数据库，但因业务表和初始化尚未完成，所有/health及/api请求仍保持503。实际连接、建表和恢复检查全部成功后才可开放就绪状态。任务1.3完成不表示S29完整场景通过。

### D04：数据结构与样例

| 表 | 第一轮用途及关键字段 |
| --- | --- |
| `classes` | 班级：id、name |
| `users` | 账号：id、username（唯一）、password_hash、role、class_id |
| `sessions` | 会话：token_hash、user_id、expires_at |
| `materials` | 材料：id、class_id、uploaded_by、title、original_filename、storage_key（NOT NULL，全表唯一约束）、size_bytes、sha256、source_url（可空）、source_locator（可空）、created_at |
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

响应优先顺序：材料接口先检查服务是否就绪；启动恢复或D07所述不确定状态期间，无论是否携带Cookie、属于何种角色，一律返回503、code=SERVICE_UNAVAILABLE、message=“服务暂时不可用，请稍后再试”，不读取或返回材料，不执行上传。此状态优先于正常情况下的401/403/404，不表示放行权限。恢复就绪后才依次检查会话、写请求Origin、角色、班级及请求内容，恢复原有状态码。触发未知提交的原上传请求单独按D07返回UPLOAD_RESULT_UNKNOWN，其后到来的材料请求使用SERVICE_UNAVAILABLE。

登录、退出和/api/me不因单纯材料恢复而关闭；若数据库仍可正常验证会话，继续原有规则。若这些接口或已进入处理的材料请求因数据库故障无法可靠验证身份/执行会话操作，则返回通用503且无业务数据，不能猜测身份、伪报401或声称登录/退出成功。前端收到503显示暂时不可用，不当作401强制跳转登录页。除场景明确注入异常外，正常接口状态码验收以服务就绪、依赖可用为前提；S38单独验收上述优先顺序。

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
5. 只有确认事务未提交且已结束，才回滚可回滚的操作并删除本次新增文件。提交超时、连接断开或未收到确认不等于明确失败；不得因此直接删文件。成功后刷新列表、查看和下载都从实际持久化数据读取。

磁盘文件与 MySQL 不是同一种事务资源：数据库回滚不会自动删除文件。proposal所述“失败时回滚并清理”仅指已确认未提交的失败，不包括结果不确定的情况。不能宣称已实现跨文件系统与数据库的分布式原子事务。

提交结果不确定时，以本次服务端生成的唯一storage_key核对原文件、materials和knowledge_entries；materials.storage_key须有唯一约束，不依靠标题或原文件名识别上传。首版只允许一个api实例使用上传卷，上传与恢复检查互斥执行，恢复期间不接受新上传。核对必须使用权威MySQL的新连接及最新已提交状态，不能使用旧事务快照或副本。必须先确认原事务已结束、不会再迟到提交；实现须有可验证的数据库事务结束或锁同步依据，客户端超时、关闭连接或一次查询无记录均不能单独作为该依据。无法确认则按未知处理，不做破坏性清理。

- 已确认提交且两表关联、班级、文件大小与SHA-256完整对应：保留文件；原请求仍可响应时返回201。若连接已断，不伪造送达成功；教师恢复后可刷新本班列表确认，不自动重传。
- 已确认原事务结束且未提交、数据库没有本次记录：清理本次新文件，返回通用500；删除失败则保持不就绪等待恢复。不能影响已有材料。
- 数据库不可用、事务未确认结束或暂时无法判定：保留文件，不报告上传成功；原请求仍可响应时返回503及统一错误结构，code为UPLOAD_RESULT_UNKNOWN，message为“上传结果暂未确认，请稍后刷新列表确认，勿重复上传”。前端保留此提示且不自动重试上传。应用保持不就绪，暂停材料列表、详情、下载和上传（返回503且无材料数据）；/health返回503。恢复确认后才能恢复服务。本轮不承诺用户主动再次上传的去重，同名重传仍可能生成新材料。

启动时，在/health就绪及开放材料接口之前，先确认旧上传事务已结束、无其他写入者，再双向核对本应用上传卷与双表：确认没有数据库引用的孤立文件才可删除；数据库已有材料记录但缺原文件、缺正文、关联/班级不符或文件大小/哈希不符时，不自动删除记录或伪造空文件，保留现存证据、保持不就绪并报告可诊断错误，待人工修复后重查。数据库不可查、旧事务结束无法确认或清理失败时同样不就绪，不能将“查不到数据库”当作“数据库无引用”。恢复操作可重复执行，确认一致后才开放材料接口；运行中遇到上述未知结果也执行同样的互斥恢复流程。日志仅保留不含正文、凭据或会话标识的内部诊断信息，对外不暴露存储键和路径。只处理本应用数据卷内的文件，不触碰其他路径。

每次成功上传生成新材料，即使原文件名相同也不覆盖。Markdown 首版按纯文本安全显示，禁止直接执行上传 HTML/脚本。下载使用 attachment，不暴露服务器路径。文件上传保存仍不代表已切片或已可检索。

### D08：来源信息与课程资料准备

转换得到的 `.md` 推荐保留课次、章节、原始URL和原始位置；上传时保存可提供的 source_url/source_locator。没有真实页码时不编造页码，没有原地址时留空。第一轮只保存和显示来源，答案引用与检索在第 4—5 课实现。

PPT/PDF 中的代码、命令、表格或图片解释需要在转换任务中核对。真实转换未完成前，只用明确标注的虚构样例验收上传链路，不声称课程知识库已经建立完整。

### D09：Compose、入口与数据保留

第 3 课提供 `compose.yaml`，固定项目名 `campusclaw-course`，服务 web（Nginx与构建后的前端）、api（Go）、db（MySQL）。本地仅 web 发布 `127.0.0.1:${WEB_PORT}`，端口实施前检查占用。api、db 仅内部网络可达，不发布宿主机端口。

按用户本轮确认，MySQL采用本项目F盘runtime/mysql-data目录绑定挂载到容器/var/lib/mysql；不同于此前默认命名卷方案，这样明确控制实际数据盘。仅本地课程开发使用，Windows文件系统的权限、性能及大小写行为须实测，不作为生产部署建议。镜像沿用Docker现有存储，不移动旧项目。上传数据后续单独持久化，仍仅挂载给api，Nginx不挂载、不公开uploads。初始化、种子及文件恢复检查完成后api才就绪。

数据库连接增量：compose先只实现db和按需运行的db-check检查工具；均不发布宿主端口。db-check使用本机交叉编译的Linux Go程序，在本项目网络内调用同一database模块执行Ping和SELECT 1；正常serve启动也先检查数据库，失败脱敏退出，成功仍因业务表及初始化未完成而保持503。--check-config继续不联网，--check-db只检查数据库后退出，不能以连接成功标记平台就绪。连接使用专用应用账号，仅授权本项目数据库，连接/读取/写入有超时与连接池上限，驱动错误不原样输出。初始随机密码放在忽略版本管理的runtime/private中，限制本机访问，通过只读秘密文件传入容器，不写入镜像或命令行。

本机F盘实测为可移动NTFS，Docker的WSL不自动挂载。scripts/mount-project.ps1仅把当前项目挂到新的专用入口/run/desktop/mnt/host/campusclaw-course，不覆盖已有/mnt/host/f；核对Windows与Linux的README指纹一致后才设置Compose路径。Docker/WSL重启后由脚本检查并重新接入，失败即停止，不能退回其他盘建库。数据库运行时不得移除F盘；所有容器数据路径均落到本项目目录。

普通docker compose down/up不删除F盘绑定目录和后续持久化卷，沿用同一目录与卷时数据应保留。删除数据目录、down -v删除命名卷、换用新数据目录或卷均不属于保证。验证只在本项目独立环境进行。

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

### 当前建表增量（T-002.10）
显式执行 --init-schema，仅建立D04八张表及schema_migrations结构版本表；不写入演示账号、密码或材料。已有业务行不更新、不删除。版本记录保存SQL文件SHA-256和完成时间；同一版本文件被改写时拒绝继续。建表用项目数据库命名锁串行执行，失败允许用同一版本补齐，不能将MySQL建表解释成可整体回滚的事务。首次发现无版本记录的同名业务表则拒绝接管，避免误认已有结构。完成版本缺表时拒绝继续，不悄悄重建空表。实际结构更改必须用后续版本，不修改001。
本步只验收建表重复安全；四账号bcrypt密码、两班虚构材料、原文件和完整种子幂等性留在2.2/2.3，不因建表通过而勾选它们。登录、上传及HTTP就绪仍未实现。

### 账号种子增量（T-002.11）
显式--init-accounts建立班级ID 1（A班）、2（B班）及teacher_a/student_a1/teacher_b/student_b1；ID保留给演示班，已存在且名称不符时拒绝，不改已有班级。已有同名账号须角色及班级一致，否则拒绝；一致则保持密码原值且不要求对应种子密码。仅缺少的账号读取SEED_*密码，空白或超过72字节拒绝，bcrypt cost12。所有班级及账号写入在一个事务中，通过锁定已完成的结构版本行串行执行；任何失败回滚，不用UPSERT覆盖已有值。随机初始口令保存在F盘runtime/private/seed目录，脚本只读注入环境变量，不输出。材料种子、登录、上传暂不实现。

### 登录核对增量（T-002.12）
当前POST /api/login仅核对凭据；成功200返回verified=true、session_created=false及“账号密码核对通过，尚未建立登录状态”，不返回Cookie/用户信息、不插入sessions、不授权讲义。此临时增量不代表D06/S01完整登录完成，下一步建立会话时替换成功响应。/health及其他/api仍503。
按handlers→services→repositories分层；参数化查询用户名，只取password_hash。未知用户执行同cost12的占位bcrypt校验，和错密码统一401 INVALID_CREDENTIALS/用户名或密码错误，不声称完全恒定时延。数据库/无效存储哈希返回503，不显示SQL或凭据。正确Origin才继续；方法非POST为405，缺失/不符/多个Origin为403。仅接受application/json、正文最多4096字节（超限413）；字段仅username/password，拒绝未知字段、尾随JSON、非法UTF-8及空值，用户名至多191字符、密码至多72字节，格式错误400。成功及失败均no-store。此入口仅本机/项目内网开发，不作为已具备会话与限速的公网登录服务。

### 会话增量（T-002.13，替代T-002.12临时成功响应）
POST /api/login成功返回用户id/username/role/class_id并设置campusclaw_session；32随机字节转64位十六进制，数据库仅保存SESSION_SECRET计算的HMAC-SHA256。固定8小时，GET /api/me不续期；服务端时间达到expires_at即401。用户来自数据库关联，不信任Cookie中的身份或班级。Cookie为HttpOnly、SameSite=Lax、Path=/，HTTPS来源Secure=true；MaxAge=28800及Expires一致。
密码核对后事务内锁用户并再次确认哈希未变化，再删除本请求旧会话、插入新摘要，提交成功才发Cookie。重复登录令本请求旧Cookie失效，不撤销其他设备会话。POST /api/logout校验Origin、删除有效当前会话，成功204并清Cookie；无效/过期401，数据库失败503且不伪报成功。重复同名Cookie拒绝。/health及材料仍503。受控时钟仅作为Go构造依赖供测试注入，不增加HTTP改时接口或生产环境绕过开关。
