# CampusClaw 项目导航

项目位置：`F:\学校\PKU\课程\选课\软件开发\个人作业\CampusClaw`。

第3课登录、班级权限和材料上传查看已完成本机验收；本文件按当前项目现状整理。全部第1—6课平台仍在进行中，第4课未开始。

## 常用入口

| 文件 | 用途 |
| --- | --- |
| [README.md](README.md) | 本地启动步骤、版本要求和使用说明 |
| [AGENTS.md](AGENTS.md) | 本项目操作边界和开发规则 |
| [HANDOFF.md](HANDOFF.md) | 第3课已验收事项及下一阶段交接 |
| [TASK_BOARD.md](../TASK_BOARD.md) | 工作区总任务及T-002.14状态 |
| [proposal.md](openspec/changes/add-auth-rbac-class-knowledge/proposal.md) | 本次功能范围 |
| [design.md](openspec/changes/add-auth-rbac-class-knowledge/design.md) | 技术设计与实施取舍 |
| [spec.md](openspec/changes/add-auth-rbac-class-knowledge/specs/auth-upload/spec.md) | 各场景验收标准S01—S43 |
| [tasks.md](openspec/changes/add-auth-rbac-class-knowledge/tasks.md) | 实施和验收任务记录 |
| [MATERIALS.md](docs/lesson-03/internal/MATERIALS.md) | 本批功能和场景验收报告 |
| [SESSIONS.md](docs/lesson-03/internal/SESSIONS.md) | 登录会话验收记录 |
| [materials-persistence-verification.json](docs/lesson-03/internal/materials-persistence-verification.json) | 容器重建前后材料编号、正文和下载摘要 |

## 项目结构

```text
frontend/                 React登录、列表、详情和教师上传页面
backend/                  Go服务入口、数据库、会话、材料服务与测试
deploy/nginx/             同源网页与API入口
scripts/                  本机开发、构建、数据库和集成检查脚本
docs/lesson-03/materials/  只读挂载的演示材料种子
docs/lesson-03/internal/  环境、数据库、账号、会话和本批内部验收证据
openspec/                 本次变更规约与验收记录
runtime/                  本地数据库、上传文件和私密配置；不提交版本库
compose.yaml              本项目web/api/db容器定义
```

本机入口为 `http://127.0.0.1:8080`，只有网页服务发布宿主端口。此导航是静态文件说明；新增实现以目录内容和任务板为准。
