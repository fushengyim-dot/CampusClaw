# CampusClaw：软件开发课程学习平台

- **价值：**让本门软件开发课程的学生在本班资料范围内学习概念、理解实操和整理知识，并能核对回答出处。
- **核心场景：**教师上传课程资料；学生使用讲义问答、解题引导、复习整理三个应用。第一轮先完成登录、权限、班级隔离与讲义上传保存。
- **本期不做：**第 7 课及以后的完整作业、批改、成绩、异步队列等流程；第一轮另外不做检索、AI 对话、提示词模板、技能运行和 MCP，这些在第 4—6 课逐步加入。

## 当前阶段

当前仅有第 2 课的项目说明与规约，没有业务代码，不能启动网站。本轮规约等待用户确认，第 3 课才按清单实现。

本项目是当前目录下独立的新仓库，不是旧的 `F:\CampusClaw`，不读取或迁移旧数据库、凭据或运行状态。

## 本期五项功能

1. 多个内置 AI 应用：讲义问答、解题引导、复习整理。
2. 可选择并填写的提示词模板。
3. 本班知识库检索与真实资料出处。
4. 内置技能及上传技能，支持校验、绑定与启停。
5. 外部工具及 MCP 接入。

这些是第 1—6 课整期目标，不表示本次变更已经实现。教材来源为老师这门课的网页、课件和实操教程；数据准备由助手负责转换、核对来源，第一轮上传只接收 UTF-8 `.txt`、`.md`。

## 这次先读哪些文件

变更名称：`add-auth-rbac-class-knowledge`。

| 文件 | 通俗解释 |
| --- | --- |
| `openspec/changes/add-auth-rbac-class-knowledge/proposal.md` | 本轮为什么做，做哪些，不做哪些 |
| `openspec/changes/add-auth-rbac-class-knowledge/design.md` | 页面、后端、数据库怎样配合，为什么这样设计 |
| `openspec/changes/add-auth-rbac-class-knowledge/specs/auth-upload/spec.md` | 一条条能判断通过或失败的要求 |
| `openspec/changes/add-auth-rbac-class-knowledge/tasks.md` | 第 3 课按什么顺序实现，每步怎样检查 |
| `AGENTS.md` | 开发者和 AI 助手在本仓库必须遵守的规则 |
| `docs/lesson-02/internal/REVIEW.md` | 来源对照、文档检查和未完成事项，属于内部备注 |

`openspec/specs/` 目前没有已归档能力；本变更仍在 `changes/` 中，不能把规划当成已实现的系统。

## 第一轮准备采用的技术组合

React + TypeScript 前端、Go 后端、MySQL 数据库、Nginx 单入口、Docker Compose 本地启动。

登录采用服务端会话：浏览器 Cookie 只保存随机会话标识，身份与班级从服务端取得。学生上传返回 403；跨班访问与不存在的材料统一返回同样的 404 响应。第 5 课的 Bearer 令牌要求在后续变更中单独处理。

## 现在怎样检查规约

在本项目根目录运行：

```powershell
openspec validate add-auth-rbac-class-knowledge --strict --no-interactive
```

这条命令检查文档结构，不会证明登录、上传、权限或部署已经工作。运行结果见内部备注。

## 第 3 课完成后的启动约定

以下是未来实现必须提供的使用方法，当前尚不可执行：

1. 根据届时提供的 `.env.example` 在本机填写配置；示例只列变量名，不提供真实密码。
2. 执行 `docker compose up --build -d`，启动 `web`、`api`、`db`。
3. 访问配置的本机入口 `/login` 登录，访问 `/health` 查看服务是否就绪。
4. Compose 初始化数据库和验收账号；重复启动不得覆盖已有讲义或重置密码。

最终入口端口在第 3 课检查本机占用后通过配置确定。数据库与后端不直接向宿主机开放端口，上传目录不作为公开静态目录。

普通 `docker compose down` 再 `up` 应保留命名数据卷中的资料。这个保证以使用同一项目和同一数据卷、没有删除数据卷为前提；不得用 `down -v` 验证资料保留。

## 当前目录

```text
CampusClaw/
├─ AGENTS.md
├─ README.md
├─ .gitignore
├─ openspec/
│  ├─ config.yaml
│  ├─ specs/                         # 实现验收并归档后才进入这里
│  └─ changes/
│     └─ add-auth-rbac-class-knowledge/
│        ├─ proposal.md
│        ├─ design.md
│        ├─ tasks.md
│        └─ specs/auth-upload/spec.md
└─ docs/lesson-02/internal/           # 来源与检查记录
```

后续代码目录按 design.md 建立，本轮不创建空业务文件冒充实现。
