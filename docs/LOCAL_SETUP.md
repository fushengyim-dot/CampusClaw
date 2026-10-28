# 本地运行

## 环境要求

现有脚本适配 Windows PowerShell、Docker Desktop 的 WSL 2 Linux 容器环境，以及 Node.js/npm。Node.js 需支持 `--experimental-strip-types`。后端版本要求见 `backend/go.mod`。

运行前核对以下路径配置：

- `scripts/database.ps1` 和 `scripts/mount-project.ps1` 当前要求项目位于 F 盘。
- `scripts/dev.ps1` 中 `$goCommand` 指向 `F:/CodexTools/CampusClaw/go1.27.3/go/bin/go.exe`，`$cacheRoot` 指向 `F:/CodexCache/CampusClaw`。请安装对应 Go 工具，或将脚本路径调整为本机安装位置。
- 挂载脚本使用 Docker Desktop 的专用 WSL 挂载路径；其他操作系统不能直接沿用这些 PowerShell 启动命令。

## 模型配置

首次使用时复制 `.env.example` 为 `.env`，已有配置不要覆盖。填写 `CHAT_BASE_URL`、`CHAT_API_KEY`、`CHAT_MODEL`，以及向量检索所需的 `EMBEDDING_BASE_URL`、`EMBEDDING_API_KEY`、`EMBEDDING_MODEL`、`EMBEDDING_DIM`。

Base URL 填到 `/v1`，客户端补齐接口路径。Compose 读取 `.env` 并传给后端；密钥和本机秘密文件不应提交到仓库。

## 准备与启动

在仓库根目录运行，先确保 Docker Desktop 已启动、8080端口未被占用：

```powershell
./scripts/dev.ps1 -Task install
./scripts/database.ps1 -Task prepare
./scripts/database.ps1 -Task prepare-accounts
./scripts/database.ps1 -Task up
```

首次准备会生成随机秘密文件和演示账号密码，已有文件不会被覆盖。演示账号为 `teacher_a`、`student_a1`、`teacher_b`、`student_b1`，密码在 `runtime/private/seed/` 中对应的种子文件内。教师登录后可签发本班教师或学生邀请码。

浏览器打开 http://127.0.0.1:8080 。健康检查为 http://127.0.0.1:8080/health ，就绪时返回 `{"status":"ok"}`。

数据库、向量索引和上传文件位于 `runtime/`。停止服务但保留数据：

```powershell
./scripts/database.ps1 -Task stop
```

## 开发检查

```powershell
./scripts/dev.ps1 -Task test
./scripts/dev.ps1 -Task build
./scripts/dev.ps1 -Task build-linux
```

数据库检查需要已启动的本机服务，会创建并清理专用测试数据：

```powershell
./scripts/database.ps1 -Task test-schema
./scripts/database.ps1 -Task test-materials
./scripts/database.ps1 -Task test-search
```

已安装 OpenSpec CLI 时，可运行 `openspec validate --all --strict --no-interactive` 检查设计文档。

`test-chat` 和 `test-embedding` 会向配置的真实模型服务发送请求，可能消耗额度，不属于离线测试；请确认服务地址、数据范围和额度后再运行。
