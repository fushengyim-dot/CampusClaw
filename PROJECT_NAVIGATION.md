# 项目导航

| 入口 | 内容 |
| --- | --- |
| [项目介绍](README.md) | 用途、功能与技术组成 |
| [功能与处理流程](docs/PROJECT_HISTORY.md) | 教师配置、学生使用与请求处理 |
| [运行说明](docs/LOCAL_SETUP.md) | 环境、配置、启动和开发检查 |
| [界面展示](docs/evidence/browser/README.md) | 各功能页面截图 |
| [项目报告](docs/report/CampusClaw_Report_v3.pdf) | 模型请求、知识库、技能和工具说明 |

## 源码结构

- `frontend/`：页面、交互和流式消息展示。
- `backend/handlers/`：HTTP接口与权限检查。
- `backend/services/`：资料检索、对话、技能及工具处理。
- `backend/database/`：数据结构、迁移和集成测试。
- `backend/mcpserver/`：内置MCP工具服务。
- `scripts/`：运行、构建与测试脚本。
- `openspec/`：需求与技术设计。
- `compose.yaml`：容器服务和网络配置。
