# 求知 · GitChat

把问题和回答组织成可深入、发散、回溯与合并的卡片，让用户按自己的节奏思考。Go 服务内嵌中文界面，Android、iPhone/iPad、macOS 和 Windows 共用同一套体验；提供可安装网页与 Tauri 原生客户端。

## 四端使用

| 设备 | 安装网页 | 原生客户端 |
| --- | --- | --- |
| Android | Chrome 安装到主屏幕 | Android 工程与 APK 构建流程，连接 HTTPS 服务 |
| iPhone / iPad | Safari 分享 → 添加到主屏幕 | iOS 工程与模拟器构建流程；真机安装需 Apple 签名 |
| macOS | Safari 添加到程序坞，或 Chrome 安装 | `.app`，可运行随包附带的本地 Go 服务，也可连接远程服务 |
| Windows | Edge / Chrome 安装应用 | NSIS 安装包构建流程，本地 Go 服务或远程服务 |

网页侧导航中的「安装求知」提供设备对应的步骤。「设备与空间」可通过一次性配对码连接另一台设备的同一空间，并保留返回本机原空间的入口。四端需连接同一个 HTTPS 服务；电脑上的回环地址不能直接作为手机的服务器地址。

原生工程、依赖与打包命令见 [原生客户端说明](apps/native/README.md)，设计落实和平台验证边界见 [四端与体验说明](PLATFORMS.md)。

## 启动

需要 Go 1.24.4 或以上版本。

```sh
go mod download
go run .
```

打开 http://127.0.0.1:8002 。未配置模型密钥时使用明确标记的演示模式，可体验完整的卡片操作和持久化。演示回答由本地模板生成。

点击顶部模型按钮或侧栏「模型与服务」→「添加模型连接」，选择协议，填写 API 地址、密钥和模型 ID。可以读取模型列表、测试连接、保存多组配置并随时切换。首页演示提示也有直接入口。详情见 [模型接入说明](MODELS.md)。

服务端默认模型仍可通过环境变量配置，供未选择个人连接的空间使用：

```sh
export OPENAI_API_KEY='你的密钥'
export OPENAI_BASE_URL='https://api.openai.com/v1'
export OPENAI_MODEL='你的模型名称'
go run .
```

环境变量默认使用 Chat Completions；YAML `chatbot.protocol` 可选择五种协议，服务按协议追加请求路径。`jsonMode: prompt` 用于不支持 JSON 参数的兼容服务，`tokenParameter` 可选择 `max_tokens` 或 `max_completion_tokens`。旧的 `OPENAI_API_KEY_0` 命名仍受支持。

复制 `config.example.yaml` 后可通过 `GITCHAT_CONFIG=/路径/config.yaml go run .` 加载完整配置。`GITCHAT_MODE=demo` 将服务端默认设为演示模式，仍可在界面保存和启用真实模型；`GITCHAT_MODE=openai` 启用服务端默认模型（免鉴权本地服务须明确配置 `noAuth: true`）。`GITCHAT_ADDR` 控制监听地址，`DATA_DIR` 控制数据目录。

```sh
docker compose up --build -d
```

Compose 将服务映射至本机 8002 端口，并将数据保存在命名卷中。运行在局域网或公网时，配置 HTTPS 反向代理和适合部署环境的访问控制。TLS 在代理终止时设置 `GITCHAT_SECURE_COOKIES=1`。当前用户空间由 HttpOnly 随机凭据区分，清除全部 Cookie 会失去本设备的访问凭据；可从仍连接该空间的设备重新配对。

## 使用

- 上滑或点击继续追问，从当前卡片深入。左滑或选择发散，从当前卡片的父上下文创建平行话题。
- 下滑回到父卡片，右滑切换相邻思路。双指缩放或点击思维图打开总览。
- 总览按层展示卡片及关系，勾选多张卡片后合并提问。合并上下文保留各分支结论，并对共同祖先去重。
- 灰色预测话题按相关性排序，点击后生成新的探索卡片。回到历史节点仍可展开未选的话题。
- 选中回答中的文字后点击引用，可在下一张提问卡片中保留引文。
- 收藏、标记想清楚、归档与恢复均会保存。卡片菜单支持复制回答、从节点复制新对话和分享快照。
- 分享链接包含所选节点及其上游图结构。接收方可独立复制后继续提问。链接需要在能访问同一服务的环境中使用。
- 偏好设置支持语气、兴趣与常见任务。模型从用户明确表达中提取的稳定偏好也会保存，并用于后续生成。
- 思维图默认聚焦当前分支，每层可单独切换相邻卡片；需要时可展开全部。隐藏在当前行外的合并选择仍会保留，最多选择 16 张卡片。
- 提问阶段的建议先填入输入框，允许修改后发送；发散卡片准确显示同层父背景，合并卡片显示各个来源。
- 手机回车换行，使用发送按钮提交；电脑 Enter 发送、Shift + Enter 换行，Cmd/Ctrl + Enter 也可发送。Escape 返回回答，非输入状态下 Alt + 方向键可切换思路。
- 安装网页后，断网重开可先把灵感留在本机，联网后再放进提问卡片编辑。灵感不会自动发给模型；历史对话仍由服务器保存，离线外壳不缓存私人 API。

## 模型与上下文

每轮模型返回结构化的标题、简短回答、2–4 个带评分的建议和目标/进展/分支记忆。首层回答上限为 600 字符，深入后的上限可配置。服务校验结构、长度和建议后发送 SSE 文本分段，首段显示前需要等待模型生成及校验。失败采用有限次数重试，可配置备用模型。

界面选择的个人连接固定使用所选模型，不继承服务端的跨模型路由；已开始的回答使用开始时的配置。

服务端默认连接的 `routing` 支持按关键词选择专长模型，按深度、合并操作和上下文体量升级到强模型。长历史及模型切换时会调用模型生成摘要，完整原始卡片保存在本地，供回顾和恢复。路由采用可配置规则，实际成本取决于服务商计价与模型输出。

`privacy.redact` 在发给远端前处理常见邮箱、手机号、身份证号和密钥模式。`privacy.modelCheck` 可额外调用模型检查脱敏后的上下文，检测到剩余敏感内容时返回可见错误。该模型检查本身使用配置的远端服务。本地原始对话按原文保存。

`knowledgeDir` 可指向本地 Markdown/文本知识库。检索采用中英文关键词和中文双字匹配，返回排名靠前的片段，并要求回答标明 `[source: 文件名]`。每次扫描最多 1000 个文件，忽略符号链接及超过 256 KiB 的文件。

`mcpServers` 可接入 MCP 2025-03-26 Streamable HTTP 服务，执行初始化、工具发现和显式配置的只读查询工具。工具须声明 `readOnlyHint`，接受 `{ "query": "问题" }` 参数。每个服务每轮最多调用三个允许的工具，结果长度有上限。服务端令牌通过 `tokenEnv` 对应的环境变量读取。协议范围依据 [MCP 2025-03-26 传输规范](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-03-26/basic/transports.mdx)。

## 数据与可靠性

默认数据文件为 `data/gitchat.json`，包含会话、分享快照、偏好、动作统计与加密的模型连接。模型密钥由数据目录中的 `model-key.bin` 加密，备份时必须一并保存；持有两份文件的人可以解密，部署环境仍需保护数据目录。写入使用临时文件、同步和原子重命名，同一数据目录由一个服务进程独占。关闭服务后复制数据目录即可备份，恢复时放回同一目录。分享快照独立于源会话，删除源会话后仍可访问已生成的分享链接。

每张卡片有独立模型请求及取消上下文。客户端断连会取消生成，服务重启会将未完成卡片标记为已中断。请求可携带 `request_id` 实现幂等重放，同一 ID 对应不同内容会返回冲突。各浏览器空间间的会话访问经过所有权校验。

切换焦点时，会将距焦点超过三条图边、24 小时未活跃且未被收藏或标记完成的非祖先卡片归档。归档保留内容和关系，可从历史界面恢复。动作统计记录深入、发散、合并、收藏等行为，并可通过 API 查看。

## API

接口返回 JSON，错误返回 `{ "error": "说明" }`。浏览器需要保留 `gitchat_owner` Cookie。

| 路径 | 用途 |
| --- | --- |
| `GET /health` | 存活检查 |
| `GET /api/config` | 当前空间的模型、协议、连接名与可用应用 |
| `GET/POST /api/model-connections` | 列出或新增模型连接，读取时不返回密钥 |
| `PUT/DELETE /api/model-connections/:id` | 编辑或删除连接，检查 revision 防止覆盖 |
| `POST /api/model-connections/test` | 用草稿配置发送简短测试，不保存 |
| `POST /api/model-connections/models` | 读取模型列表，不保存 |
| `PUT /api/model-selection` | 选择 server、demo 或已保存连接 ID |
| `GET/POST /api/sessions` | 列出或创建会话 |
| `GET/DELETE /api/sessions/:id` | 读取或删除会话 |
| `POST /api/sessions/:id/chat` | 创建卡片并接收 SSE |
| `POST /api/sessions/:id/cards/:cardID/stop` | 停止生成 |
| `PATCH /api/sessions/:id/cards/:cardID` | 收藏、归档、完成标记 |
| `POST /api/sessions/:id/focus` | 设置焦点 |
| `POST /api/sessions/:id/copy` | 复制指定节点上游对话 |
| `POST /api/sessions/:id/share` | 创建分享快照 |
| `GET /api/shares/:token` | 读取分享快照 |
| `POST /api/shares/:token/import` | 导入为独立会话 |
| `GET /api/sessions/:id/export` | 导出 JSON |
| `GET/PUT /api/preferences` | 读取或保存偏好 |
| `GET /api/stats` | 查看动作统计 |
| `GET /api/devices` | 查看能否返回本机原空间 |
| `POST /api/devices/pairing` | 生成 10 分钟有效的一次性配对码 |
| `POST /api/devices/pair` | 使用 `{ "code": "…" }` 连接另一空间 |
| `POST /api/devices/restore` | 返回本机原空间 |
| `POST /api/devices/disconnect` | 转入新的空白空间，保留原空间恢复凭据 |

创建会话接受 `{ "app_id": "default", "title": "旅行规划" }`。`chat` 请求示例为

```json
{
  "operation": "deepen",
  "parent_ids": ["父卡片 ID"],
  "question": "应该怎样选择路线？",
  "quote": "",
  "request_id": "客户端生成的唯一 ID"
}
```

`operation` 可为 `deepen`、`branch` 或 `merge`。`branch` 传当前卡片 ID，服务解析它的父节点以创建同层话题。`merge` 接受 2–16 个不同卡片 ID。SSE 每帧的 `data` 是 JSON，`type` 为 `created`、`delta`、`completed` 或 `error`。终态 `card.status` 可为 `completed`、`cancelled` 或 `error`。

原有 `POST /g/:appid`、`POST /g/:appid/:session` 保留，使用 `message.content[].text[]` 及 `{ "MainId": "…" }` 形式的 ID。`stream:false` 返回 JSON，`stream:true` 返回 `event: stream`。`tests/curl.sh` 提供兼容接口示例。

## 验证与代码结构

```sh
go test -race -timeout 60s ./...
go vet ./...
go build ./...
node --check web/app.js
```

测试使用本地模拟模型/MCP 服务，覆盖真实协议解析、模型路由及回退、上下文隔离、合并、取消、幂等、重启恢复、写入失败、用户空间隔离和 HTTP 完整流程。真实服务测试需显式设置 `GITCHAT_LIVE_TEST=1`、模型密钥及对应配置。

- `src/cards` 管理对话 DAG、上下文、持久化和兼容入口。
- `src/modelServer` 管理模型、输出校验、检索和 MCP。
- `src/config` 加载 YAML 与环境变量。
- `src/router.go` 提供 API、SSE 和静态资源。
- `web` 包含内嵌页面、样式、交互和思维图绘制。

此实现采用单服务部署和本地持久化。文档中的付费订阅、模型训练与商业收益指标需要相应的支付、训练数据和运营系统支持。

## 浏览器可视化回归

需要 Node.js 20 或以上版本和 Chrome/Chromium。

```sh
npm ci
# 没有系统 Chrome 时安装测试浏览器
npx playwright install chromium
npm run test:e2e
```

脚本自动构建并启动真实 Go 应用，以临时数据目录和本地可控模型服务运行。桌面、手机手势、异常恢复、设计体验、四端集成、原生网页与模型配置七个套件逐项操作界面、核对持久化结果并保存截图。结果与可浏览截图报告分别位于 `test-results/results.json` 和 `test-results/report.html`。报告出现任一失败，命令会返回非零退出码。

可用 `npm run test:e2e -- mobile` 单独复测手机端，`HEADED=1 npm run test:e2e` 显示浏览器。`PLAYWRIGHT_CHROMIUM_EXECUTABLE` 可指定浏览器路径，`GITCHAT_TEST_OUTPUT` 可指定报告目录。完整范围见 [浏览器测试说明](tests/browser/README.md)。

苹果浏览器引擎可独立回归：`npx playwright install webkit` 后运行 `npm run test:webkit`。WebKit 与手机尺寸仿真可以检验兼容性，但不能替代真实 iPhone/Android 键盘、系统分享与签名安装验证。
