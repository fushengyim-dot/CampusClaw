# 浏览器页面验收截图

这些 PNG 来自 2026-10-28 对本机 CampusClaw 页面（`http://127.0.0.1:8080`）的实际浏览器操作和页面截图。截图保留页面原貌，没有用代码绘图、拼接或生成假页面。登录页面显示的是项目教师测试账号 `teacher_a`；截图未显示密码、Cookie、API Key 或 MCP 令牌。

截图能证明页面当时显示的入口和配置状态，不能单独证明后台模型收到的网络请求或模型返回内容。此轮没有发送课程模型请求。

## 页面功能

- `16-registration-login.png`：新版本登录页真实浏览器截图，显示“前往注册”入口。未填写或展示任何账号密码。注册表单页面已在本机浏览器中打开，并通过无障碍页面树确认包含邀请码、用户名和密码输入项，且没有角色/班级选择；截图文件保存时浏览器截图功能超时，因此该表单截图仍待补。

- `01-apps-home.png`、`12-apps-second-third.png`：首页三个内置应用及其入口。
- `10-prompt-template-selected.png`：从“讲义问答”应用入口打开会话后，选择“概念解释”模板。
- `02-materials-knowledge-base.png`、`13-knowledge-retrieval-boundary.png`：本班讲义、检索方式，以及页面说明的最多4段资料检索边界。
- `14-agent-bound-materials.png`：预置讲义助手的当前配置；截图中本班材料复选框均未勾选，反映当前数据库状态，不是绑定后的请求实测。
- `08-skill-management-form.png`、`09-skill-custom-text-form.png`：技能文本编辑/提交界面和两项预置技能。
- `11-skill-selected-content.png`：教师页面加载的数学解题引导技能原文片段。
- `05-mcp-tools-discovered.png`、`06-mcp-discovered-tools-and-binding.png`、`07-mcp-skill-tool-binding.png`：已连接的本机 MCP 服务、两个已发现的只读工具和技能工具绑定区域。截图中绑定项未勾选，页面调用记录为空。

## 五个验证问题对应的证据边界

1. 提示词模板与请求/返回：`10-prompt-template-selected.png`证明前端能选模板。模型请求体/返回并未实发捕获，具体内容目前以服务端实现和离线测试为依据，不能称为线上实测。
2. 知识库是否整库发送：`13-knowledge-retrieval-boundary.png`显示页面说明最多检索4段；`14-agent-bound-materials.png`显示当前讲义助手没有绑定材料。具体上限还由服务端实现与离线测试支撑。
3. Skill 内容是否整份发送：`11-skill-selected-content.png`显示技能的自定义格式。是否进入模型消息需以服务端组装逻辑和离线测试为依据；本轮未发送模型请求。
4. Skill 如何执行：`11-skill-selected-content.png`显示技能触发词和说明正文。程序路由逻辑由服务端实现/离线测试说明；截图本身不表示已经运行一次学生问答。
5. MCP 集成后的模型参数：`05`—`07`显示工具发现与当前绑定状态。当前没有绑定工具，因此没有真实模型工具声明请求；参数结构只按服务端实现与离线 stub 解释。
