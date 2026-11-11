# 求知原生客户端

同一套 Tauri 2 客户端覆盖 Android、iOS、macOS 和 Windows。手机连接 HTTPS 求知服务；桌面除了连接服务，还将 Go 后端随应用一起打包，支持本地数据和离线演示。

## 桌面开发与打包

需要 Node.js 22+、Go（版本见仓库 go.mod）、Rust stable，以及 [Tauri 平台依赖](https://v2.tauri.app/start/prerequisites/)。运行要求 macOS 13+、iOS 16.4+、Android 8+（更新的 System WebView）、Windows 10/11（更新的 WebView2）。macOS 构建需要 Command Line Tools；Windows 构建需要 Visual Studio C++ Build Tools 和 WebView2。

```sh
cd apps/native
npm ci
npm run dev
# 构建安装包，签名信息由构建环境提供
npm run build
# 只做未签名 macOS .app 验证
npm run prepare:desktop
npm run tauri -- build --bundles app
```

首次启动选择「打开本地空间」或填写服务根地址。随后启动会重新打开上次选择。桌面 `⌘/Ctrl + ,` 或应用菜单可回到连接页面；手机可从空间菜单的「连接设置」返回。退出桌面应用会先关闭 Go 服务的 stdin，等待其取消任务、保存数据并退出，超时才强制结束。父进程崩溃也会关闭 stdin，避免遗留后台进程。

本地数据位于系统应用数据目录的 `com.gitchat.qiuzhi/server`；服务首次使用系统随机分配的回环端口，并保存至 `listen-port`。后续启动复用该端口，保留同源草稿与页面状态，不对局域网开放。若端口被占用，应用显示错误，不连接现有进程也不静默换端口。开发时可通过原有模型环境变量接入真实模型，未配置密钥时明确显示演示模式。本机空间不会自动变成云空间；四端同步需连接同一 HTTPS 服务并完成设备配对。

`GITCHAT_NATIVE_TARGET` 可指定 Go sidecar 的交叉编译目标，例如 `x86_64-apple-darwin` 或 `x86_64-pc-windows-msvc`；Tauri 的 `--target` 必须与其保持一致。当前打包脚本支持 Mac arm64/x64 和 Windows arm64/x64。Windows 的 Rust 工具链与相应 C++ 编译器必须在目标机器准备好。

## 手机工程

Android 需要 JDK 21、Android SDK/NDK、Rust Android 目标；iOS 需要 macOS 上的完整 Xcode、iOS SDK、CocoaPods 和 Rust iOS 目标。生成的原生工程与构建输出不提交 Git，由锁定的 Tauri CLI 重建。

```sh
npm run android:init
npm run android:build

npm run ios:init
# 未签名的 arm64 模拟器归档；不是可直接安装到 iPhone 的 IPA
npm run ios:build
```

真机 iOS 安装和 TestFlight/App Store 分发需要 Apple 开发者团队、签名证书和描述文件；Android 正式分发需自己的 release keystore。仓库不会生成或托管发布密钥，也不会自动上架。

## 连接与隔离

- 客户端校验服务器根地址及 `/api/config`，远程仅接受 HTTPS；HTTP 仅用于 `localhost`、IPv4/IPv6 回环开发地址。
- WebView 加载整个同源站点，沿用 HttpOnly Cookie、CSRF 检查和 SSE；无需放宽 CORS 或向网页暴露文件系统。
- 只有随应用打包的连接页有三个受限命令：读取连接设置、连接已校验服务、启动固定名称的 Go 二进制。`build.rs` 显式生成应用命令权限，capability 只授权本地页面；远程 URL 没有任何 native capability。命令还二次检查调用窗口和 URL。
- 禁止用户名/密码、查询串、子路径和内部保留域名的服务地址。连接检查不跟随重定向。连接页只存服务 URL，不存模型密钥。
- 已连接页面只可在所选服务同源导航；外部 HTTP(S) 链接交给系统浏览器，其他协议和任意新窗口被拒绝。
- `gitchat://settings` 只负责返回应用连接页，不执行文件、命令或任意 URL。`window.__GITCHAT_NATIVE__` 仅是 UI 标记，不提供原生权限。

## 验证

```sh
# 没有系统 Chrome 时先安装测试浏览器
npx playwright install chromium
npm test
npm run prepare:desktop
npm run test:rust
```

Node 测试检查权限配置、图标/打包契约和连接页交互；Rust 测试验证 URL 与导航策略。仓库的 `Four-platform clients` 工作流在 macOS/Windows 原生执行 Go 测试并打包，生成 Android 调试 APK 与未签名 iOS 模拟器归档。CI 构建成功不等价于真机体验验证或商店审核通过，签名分发前还需在实际设备检查键盘、分享、返回手势与网络切换。

参考：[Tauri capabilities](https://v2.tauri.app/security/capabilities/)、[AppManifest 权限生成](https://docs.rs/tauri-build/latest/tauri_build/struct.AppManifest.html)、[sidecar](https://v2.tauri.app/develop/sidecar/)、[平台分发](https://v2.tauri.app/distribute/)。
