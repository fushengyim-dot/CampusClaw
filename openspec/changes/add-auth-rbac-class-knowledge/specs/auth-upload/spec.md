# 登录、权限、班级隔离与讲义上传规约

## Purpose

为本门软件开发课程建立教师和学生都可使用、资料按班级隔离的基础系统。第一轮知识库只保存正文，不提供检索或 AI 回答。

本文 MUST 表示“必须”，MUST NOT 表示“禁止”。每个 Scenario 是一项未来运行时可判断通过或失败的场景；本文件存在不等于场景已通过。

## ADDED Requirements

### Requirement: R01 Account login

系统 MUST 支持 teacher/student 两类账号密码登录，使用服务端会话；角色与班级 MUST 来自服务端用户记录，Cookie MUST 仅携带随机会话标识。

#### Scenario: S01 Valid teacher and student login
- **WHEN** 分别使用预置教师 A、学生 A1、教师 B、学生 B1 的正确账号密码，携带有效 Origin 调用 `POST /api/login`
- **THEN** 每次请求 MUST 返回200并设置会话 Cookie，`GET /api/me` MUST 返回对应用户和班级
- **AND** 数据库 MUST 有关联该用户的有效会话，响应JSON MUST NOT 包含密码哈希、秘密配置或原始会话字段；会话标识仅通过HttpOnly Cookie下发

#### Scenario: S02 Indistinguishable invalid credentials
- **WHEN** 分别提交不存在的用户名，以及存在用户名配错误密码
- **THEN** 两种请求 MUST 均返回401、相同 JSON 结构与“用户名或密码错误”消息
- **AND** MUST NOT 建立新有效会话，不回显输入口令或披露账号是否存在

#### Scenario: S03 Rotate session on login
- **WHEN** 浏览器携带已有或预置 Cookie 完成一次成功登录
- **THEN** 系统 MUST 换发与旧值不同的新会话标识，并使该请求携带的旧有效会话失效
- **AND** 新 Cookie MUST 设置 HttpOnly、SameSite=Lax、Path=/；标识 MUST 来自至少32随机字节，Cookie中 MUST NOT 包含角色或班级

### Requirement: R02 Protected access and logout

系统 MUST 在每次受保护 API 请求上验证会话。有效期 MUST 为登录起8小时；退出 MUST 在服务端使当前会话立即失效。

#### Scenario: S04 Unauthenticated access
- **WHEN** 无 Cookie、伪造 Cookie 或已过期 Cookie 请求 `/api/me`、材料列表、详情或下载
- **THEN** 受保护 API MUST 返回401，响应 MUST NOT 包含任何材料标题、正文或存储路径
- **AND** 未登录用户打开 `/materials` 或材料详情页面时 MUST 被引导到 `/login`，页面 MUST NOT 显示业务数据；不要求静态页面壳返回HTTP302

#### Scenario: S05 Logout and replay
- **WHEN** 已登录用户以有效 Origin 调用 `POST /api/logout`，随后重新使用原 Cookie 请求材料列表
- **THEN** 退出 MUST 返回204并清除 Cookie，原会话记录 MUST 被删除，重放请求 MUST 返回401

#### Scenario: S06 Session expiration
- **WHEN** 受控检查时钟已达到会话创建时间加8小时，使用该会话访问材料
- **THEN** 系统 MUST 返回401，且 MUST NOT 通过读取操作自动延长此会话有效期

### Requirement: R03 Role authorization

系统 MUST 在服务端区分角色。教师可上传、查看和下载本班资料；学生只读。第一轮 MUST NOT 提供材料编辑、删除、覆盖接口。

#### Scenario: S07 Student upload rejected before writes
- **WHEN** 学生 A1 携带有效会话和 Origin 直接调用上传接口，即使绕过网页按钮或提供伪造 teacher 字段
- **THEN** 系统 MUST 返回403
- **AND** 本次请求前后 materials、knowledge_entries 记录及上传文件集合 MUST 不变

#### Scenario: S08 Teacher upload permitted
- **WHEN** 教师 A 使用有效会话和 Origin 上传符合R06的文本文件
- **THEN** 系统 MUST 允许上传流程并在完整保存后返回201，而学生页面 MUST 不显示可操作的上传入口

### Requirement: R04 Class isolation

所有材料列表、详情、下载和正文读取 MUST 在服务端按会话身份限定班级。客户端提供的 class_id MUST 被丢弃，角色字段 MUST NOT 改变服务端授权。

#### Scenario: S09 Class scoped list including pagination
- **WHEN** 教师 A 或学生 A1 请求材料列表，包含翻页或额外提供 B 班 class_id 的情况
- **THEN** 所有返回记录及总数 MUST 只计算 A 班材料，不包含 B 班可区分标题
- **AND** 数据 MUST 来自数据库带班级条件的查询，不能仅靠前端隐藏不属于本班的结果

#### Scenario: S10 Cross class ID matches nonexistent ID
- **WHEN** A班教师或学生分别请求B班材料与不存在材料的详情和下载地址
- **THEN** 两种情况 MUST 返回相同404状态、错误码、消息和字段结构
- **AND** MUST NOT 返回跨班标题、正文、文件大小、来源、文件路径或原文件内容

#### Scenario: S11 Forged upload scope is ignored
- **WHEN** 教师 A 上传时额外提交 B 班 class_id 或其他用户标识
- **THEN** 成功生成的 materials 与 knowledge_entries MUST 仍属于A班，上传者 MUST 是教师 A
- **AND** B班列表和数据库内B班材料数量 MUST 不因本次上传而改变

### Requirement: R05 Material browsing and download

系统 MUST 提供从数据库读取的本班列表、正文详情及受保护的原文件下载，显示标题和可用的来源信息，MUST NOT 暴露服务器存储路径。

#### Scenario: S12 Read newly uploaded material
- **WHEN** 教师 A 上传成功，学生 A1 刷新列表后打开材料详情并下载
- **THEN** 列表 MUST 出现该材料，详情 MUST 显示已入库正文，下载文件 SHA-256 MUST 与上传原件一致
- **AND** 列表/详情 MUST NOT 泄露 storage_key、数据库连接信息或会话信息

#### Scenario: S13 Empty list and pagination
- **WHEN** 在隔离检查环境清空某班材料后由该班用户请求列表，或用户请求合法/非法分页参数
- **THEN** 空列表 MUST 返回200和 `items: []`，页面显示“暂无讲义”
- **AND** 默认页码 MUST 为1、默认每页20条、最多100条，按 created_at 与 id 倒序；小于1或超过100的 page_size、非正整数页码 MUST 返回400

#### Scenario: S14 Text is not executed
- **WHEN** 获准查看的 `.md` 正文含HTML标签或脚本字符串，或标题/原文件名含特殊字符
- **THEN** 页面 MUST 安全显示文本而不执行其中脚本，下载 MUST 作为附件返回
- **AND** 请求上传文件的猜测静态路径 MUST NOT 取得原文件，原文件只能通过带权限检查的下载接口取得

### Requirement: R06 Upload validation

系统 MUST 只接受合法UTF-8（可带BOM）的 `.txt`、`.md` 文件，解码后正文非空且不含NUL。单文件 MUST 不超过5,242,880字节，整个上传请求 MUST 不超过6,291,456字节。

#### Scenario: S15 Supported formats and size boundary
- **WHEN** 教师上传有效 `.txt`、`.md` 或大写等价扩展名文件，文件大小分别为正常大小与5,242,880字节，且总请求未超限
- **THEN** 有效文件 MUST 被接受，返回201，正文 MUST 按UTF-8保留（允许去除BOM，不改写代码或命令文本）

#### Scenario: S16 Invalid type encoding or empty content
- **WHEN** 教师上传 `.pdf`、`.pptx`、`.exe`、无扩展名文件，或零字节、纯空白、含NUL或非法UTF-8的文本文件
- **THEN** 不支持类型 MUST 返回415，非法/空文本 MUST 返回400
- **AND** 文件、材料记录和正文记录 MUST 无新增

#### Scenario: S17 Oversized file or request
- **WHEN** 文件达到5,242,881字节，或总请求超过6,291,456字节
- **THEN** 系统 MUST 返回413且不创建材料、正文和原文件
- **AND** Go服务 MUST 独立检查文件上限，不能仅依赖Nginx限制

#### Scenario: S18 Metadata validation
- **WHEN** title为空白或超过200字符，source_url非http/https或超过2048字符，source_locator超过200字符，或缺少file
- **THEN** 系统 MUST 返回400且不产生文件或数据库记录
- **AND** 合法的可选source_url、source_locator MUST 可留空，服务器 MUST NOT 主动抓取source_url

### Requirement: R07 File and database consistency

成功上传 MUST 同时保存原文件、materials与knowledge_entries记录，两张表 MUST 在同一MySQL事务提交并使用相同班级。存储路径 MUST 由服务端生成。

#### Scenario: S19 Successful ingestion is linked
- **WHEN** 教师完成一次有效上传并收到201
- **THEN** materials与knowledge_entries MUST 各增加一条且通过material_id对应，class_id MUST 一致，原文件 MUST 存在
- **AND** knowledge_entries.content MUST 为本次解码正文，材料SHA-256 MUST 对应原文件

#### Scenario: S20 Failures leave no partial upload
- **WHEN** 在独立检查环境分别令文件写入失败、正文插入失败或事务提交明确失败
- **THEN** 请求 MUST 返回通用500错误，两张表 MUST 不保留本次不完整记录，本次新建文件 MUST 被清理
- **AND** 已有材料 MUST 不受影响，响应 MUST NOT 泄露SQL或文件路径

#### Scenario: S21 Safe path and repeated filename
- **WHEN** 教师上传同名文件两次，或原文件名包含路径分隔/上级目录字符串
- **THEN** 两次合法上传 MUST 使用不同服务端生成存储名并产生不同材料ID，不覆盖旧内容
- **AND** 任何生成路径 MUST 位于本应用上传卷内，原文件名 MUST NOT 被当作目录或存储路径使用

#### Scenario: S22 Recovery of unreferenced files
- **WHEN** 应用在文件写入后、数据库提交前异常退出，重启时发现本应用上传卷中没有材料记录引用的孤立文件
- **THEN** 应用 MUST 在重新接受上传前清理确认未引用的文件；清理失败时 MUST 保持不就绪并报可诊断错误
- **AND** MUST 保留数据库引用的正常文件，不能删除其他应用或卷外资料

### Requirement: R08 Core structures and seed data

系统 MUST 建立班级、用户、讲义、作业、助手、技能六类实体结构及sessions、knowledge_entries支持表。作业、助手、技能 MUST 仅保留空结构，不实现业务。初始化 MUST 幂等。

#### Scenario: S23 Initial dataset
- **WHEN** 在本项目空的独立数据库与上传卷中执行首次初始化
- **THEN** MUST 存在A/B两班，各班各一个教师和学生，账号为teacher_a/student_a1/teacher_b/student_b1
- **AND** 每班 MUST 有标题可区分且标为验收样例的材料，原文件、材料与正文一致；八张设计表 MUST 存在，三个占位表 MUST 无业务记录

#### Scenario: S24 Initialization does not overwrite data
- **WHEN** 已有种子数据且教师新增上传后再次执行初始化或重启
- **THEN** MUST NOT 重复创建账号和种子材料、重置已有密码、删除新增上传或重建清空数据库

### Requirement: R09 Password and configuration protection

系统 MUST 使用bcrypt存储和验证密码，初始cost=12，输入超过72字节 MUST 拒绝而非截断。密码、密钥和数据库凭据 MUST 从服务端环境变量读取，MUST NOT 提交或公开。

#### Scenario: S25 Hashed password and verification
- **WHEN** 查询初始化后的用户密码字段并验证正确和错误密码
- **THEN** MUST 只有可识别的bcrypt哈希，不存在明文password列或明文值；正确密码登录成功，错误密码失败
- **AND** 超过72字节的密码 MUST 被明确拒绝；查询检查记录 MUST 不打印完整哈希或口令

#### Scenario: S26 Missing configuration fails explicitly
- **WHEN** 缺少SESSION_SECRET或数据库必需配置启动，或首次初始化缺少任一预置账号密码
- **THEN** 启动或首次初始化 MUST 明确失败且保持不就绪，MUST NOT 使用硬编码默认值

#### Scenario: S27 Secrets do not leak
- **WHEN** 检查源码、Git待提交文件、配置样例、镜像构建输入、登录及材料响应JSON和运行日志
- **THEN** MUST 找不到真实密码、原始会话标识、SESSION_SECRET或数据库凭据；会话标识经HttpOnly Cookie下发是登录所需的明确例外，不得进入响应正文或前端脚本
- **AND** `.env` MUST 被Git及镜像构建排除，`.env.example` MUST 只含变量名与说明，配置缺失提示只显示变量名

### Requirement: R10 Same origin state changes

登录、退出和上传接口 MUST 对请求Origin与APP_ORIGIN进行严格一致校验；来源不符或缺失 MUST 拒绝。HTTPS使用时Cookie MUST Secure，本机HTTP示例可关闭Secure。

#### Scenario: S28 Reject untrusted origin
- **WHEN** 对登录、退出、上传发送缺失Origin或Origin不同的请求，即使带有效会话
- **THEN** 请求 MUST 返回403且不改变会话、材料记录或文件
- **AND** 同一请求携带有效Origin并满足其他权限时 MUST 可按对应正常场景处理

### Requirement: R11 Docker Compose and health

系统 MUST 提供Compose启动web、api、db的标准方式和免认证 `/health`。仅web发布本机入口，db和api MUST NOT 映射宿主机端口。配置完成和数据就绪前 MUST NOT 报健康。

#### Scenario: S29 Start and readiness
- **WHEN** 按已实现版本README填写本机配置并执行 `docker compose up --build -d`
- **THEN** 初始化完成后登录页 MUST 可访问，`GET /health` MUST 无需登录返回200且包含 `status: ok`
- **AND** 数据库不可用或数据结构未准备时 MUST 返回503、不重定向、不泄露连接细节

#### Scenario: S30 Internal services and protected uploads
- **WHEN** 检查解析后的Compose配置、容器挂载与入口访问
- **THEN** MUST 仅web发布宿主机端口，api和db只在内部网络；uploads卷 MUST 仅供api使用，Nginx MUST 不提供上传目录静态访问

### Requirement: R12 Persistent data across container recreation

MySQL数据与上传原文件 MUST 分别使用持久化命名卷。同项目同数据卷的普通down/up MUST 保留已提交资料；删除卷或更换到新卷不在保证范围。

#### Scenario: S31 Recreate without deleting volumes
- **WHEN** 教师上传资料后记录其ID与文件SHA-256，执行普通 `docker compose down` 再 `docker compose up -d`，保持项目名且不删除数据卷
- **THEN** 原账号 MUST 能登录，原材料ID、正文和文件哈希 MUST 不变，本班学生仍可访问，其他班仍被拒绝

### Requirement: R13 Provenance and honest stage boundaries

材料的source_url、source_locator等来源信息 MUST 按输入如实保存，未知留空。系统 MUST NOT 编造页码、引用、已检索或已回答状态。本轮 MUST 仅保存原文。

#### Scenario: S32 Preserve supplied source
- **WHEN** 教师上传已转换并核对的课程文本，附带真实原文URL和章节位置
- **THEN** 本班详情 MUST 原样显示可用来源字段；未提供的字段 MUST 留空而不是补造页码
- **AND** 本轮 MUST NOT 展示“AI已回答”“已建立向量索引”等未实现功能状态，虚构样例 MUST 有明确标记

### Requirement: R14 Reproducible instructions and evidence

第3课实现结束时，README MUST 提供从配置到启动、登录、上传、health和普通重启的准确步骤。验收记录 MUST 对应本文件场景与实际代码版本，未执行项 MUST 明确标出。

#### Scenario: S33 Repeat from documented steps
- **WHEN** 在独立的干净验收环境按README操作，使用本机填写的配置和预置账号
- **THEN** 操作者 MUST 能完成启动、教师上传、学生查看及下载、跨班拒绝、普通重启后数据保留
- **AND** 记录 MUST 标明代码版本、执行步骤、结果与脱敏证据，不得用OpenSpec文档校验通过代替运行验收
