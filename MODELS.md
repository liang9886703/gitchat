# 模型与服务

顶部模型按钮、侧栏「模型与服务」和首页演示提示都可以进入配置。四端共用此面板；桌面本地模式把配置存在本机 Go 服务，手机或远程模式把配置存在所连接的服务。配对到同一空间后共用连接，不同空间相互隔离。

1. 添加连接，填写便于识别的名称并选择协议。
2. 填写 API 根地址和密钥。地址也接受常见完整请求路径，会规范化为根路径。免鉴权的本地服务可留空密钥。
3. 点击「读取模型」或手动填写服务商提供的模型 ID。列表只返回最多 500 个；服务端分页还有更多时会提示，支持手动输入其他 ID。
4. 点击「测试连接」。测试实际发送固定的简短问题，并检查标题、回答、建议和记忆的卡片结构，不发送用户对话。测试与普通调用一样可能产生服务商费用。
5. 「保存并使用」立即应用于下一张卡片；「仅保存」不改变当前选择。最多保存 12 个连接。演示模式与服务端默认也可随时选回。

测试成功只代表当时的配置有效。保存不会自动发起模型请求；切换过程中已开始的回答继续使用开始时的配置。模型 ID 不预设为“最新”，以服务商的可用列表和账号权限为准。

| 协议 | 请求与鉴权 | 流式响应 |
| --- | --- | --- |
| OpenAI Chat Completions / 兼容服务 | `/chat/completions`，Bearer | SSE `choices[].delta.content` |
| OpenAI Responses | `/responses`，Bearer，`store:false` | SSE `response.output_text.delta` + completed |
| Anthropic Claude Messages | `/messages`，`x-api-key`、`anthropic-version: 2023-06-01` | SSE text_delta、message_delta、message_stop |
| Google Gemini generateContent | `/models/{model}:generateContent`，`x-goog-api-key` | streamGenerateContent + SSE |
| Ollama 原生 | `/api/chat`，可选 Bearer | NDJSON，必须收到 done |

这五种是不同的序列化与解析实现，支持普通 JSON 和流式结果。Claude/Gemini 将应用 system 指令与历史分开；Responses 不请求服务商持久化。思考文本不作为卡片答案。中途断流、输出达到上限、拒绝与非成功 HTTP 状态返回错误，不静默切换为演示回答。当前范围为文本卡片，不包含图片上传、音频、Azure 部署鉴权、Bedrock 签名或外部工具调用协议。

高级设置提供流式开关、5–120 秒超时、128–65536 最大输出 tokens、JSON/提示词兼容模式。Chat Completions 可选择 `max_completion_tokens` 或 `max_tokens`；不同兼容服务需要不同参数。Claude 使用提示词约束 JSON，其余协议在 JSON 模式下发送对应格式参数；两种模式都会做卡片结构校验。

读取模型分别调用 `/models` 或 Ollama `/api/tags`，Gemini 过滤支持 generateContent 的模型。不支持列举接口的网关可直接填模型 ID，不影响生成。界面个人连接不继承服务端强模型/备用模型/关键词路由，避免把另一服务的模型名称发给当前服务。

## 密钥与网络边界

密钥只在写入和测试请求时从浏览器发送到求知服务，不进入 URL、localStorage、分享、导出或读取 API。列表仅返回 `has_key`。编辑时空白保留原密钥，勾选清除才删除；更换协议或地址必须重新填写密钥或明确清除，防止把旧密钥送到新服务。HTTP 错误仅显示状态和说明，不回显上游响应体。已配置状态不等于已联网测试成功。

持久化使用 AES-256-GCM，附加认证数据绑定空间及连接 ID；数据目录的 `model-key.bin` 为 0600 权限。此方案不等于系统钥匙串：拥有该文件和数据库的人可解密。备份须复制整个数据目录，丢失密钥文件时失败关闭，不自动创建新密钥覆盖旧凭据。它只保护磁盘上的模型密钥，原始卡片仍按原文保存。

UI 配置的网络请求禁止重定向，禁用环境代理，并在实际拨号时解析和验证目标 IP。公网/局域网监听的服务只允许公共 HTTPS 模型地址，避免匿名空间利用服务访问内网。仅当求知监听回环地址、请求也来自本机回环且 Host 为本机时，允许本地 HTTP/内网模型；链路本地和元数据地址仍被拒绝。管理员环境变量的默认模型属于可信配置，仍可用于远程部署连接私有模型。

配置操作检查 `revision`，旧页面修改返回冲突；写请求还沿用同源检查与空间指纹检查。连接测试最多同时执行四个。没有预置真实服务商密钥，自动化测试用本地协议服务器验证，不代表已对任一付费账号完成公网调用。

## 协议依据与验证

- [OpenAI Responses API](https://developers.openai.com/api/reference/resources/responses/methods/create)、[结构化输出](https://developers.openai.com/api/docs/guides/structured-outputs?api-mode=responses)
- [Claude Messages](https://platform.claude.com/docs/en/api/messages/create)、[流式事件](https://platform.claude.com/docs/en/build-with-claude/streaming)、[API 版本](https://platform.claude.com/docs/en/api/versioning)
- [Gemini generateContent](https://ai.google.dev/api/generate-content)
- [Ollama Chat](https://docs.ollama.com/api/chat)

Go 测试涵盖五种协议的普通/流式请求契约、错误及截断、网络边界、凭据加密/重启恢复/隔离、版本冲突。`node tests/browser/run.mjs models` 使用真实求知服务和本地多协议服务器，从界面读取列表、测试、保存、生成卡片、切换、编辑、删除，并验证移动布局及未保存配置保护。`--webkit models` 运行相同流程于 WebKit。
