# Nacos Config Sync Tools

基于 **Go + Wails v2** 开发的跨环境 Nacos 配置差异扫描、结构化对比与安全同步桌面工具。

适用于在 `local`、`dev`、`test`、`prod` 等不同环境或不同 Nacos 实例/命名空间（Namespace）之间对比并同步 `.yml` / `.yaml` 配置文件，避免手工比对 YAML 遗漏配置或误覆盖目标环境的数据库、Redis、MQ 等敏感连接信息。

---

## ✨ 核心特性

- **多版本 Nacos 接口兼容**
  - 支持 `v1`、`v2`、`v3`（Nacos 3.x Console API）及 `auto` 自动探测回退模式。
  - 自动适配带 `/nacos` 上下文路径与根路径部署的 Nacos 服务端。
- **智能 DataID 环境映射**
  - **环境专属配置**：自动将 `service-{sourceEnv}.yml` 映射为 `service-{targetEnv}.yml`（如 `user-server-local.yml` → `user-server-dev.yml`），并自动跳过非当前源环境的配置。
  - **共享公共配置**：无环境后缀的配置文件（如 `common.yml`）自动保持同名映射。
- **YAML 结构化对比与保序合并**
  - 按 YAML 键路径（如 `server.port`、`spring.datasource.url`）与列表元素（List Item）级别进行精细化比对。
  - 支持**单路径应用源端修改**、**保留目标端值**、**列表项增删同步**、**一键应用全部变更**，并在合并时保持源 YAML 的键顺序。
  - 智能识别“仅目标端多出配置（Target Extra）”场景，避免误删目标环境特有配置。
- **环境敏感配置自动预警**
  - 自动检测 `JDBC`、`Redis`、`MQ`（RocketMQ / Kafka / RabbitMQ / Pulsar）、`URL`、`IP 地址`、`域名` 等环境敏感配置项，在界面中高亮提示确认。
- **忽略规则（Ignore Rules）**
  - 支持按指定 `DataID + 配置路径`（或全局路径）添加忽略规则，扫描时自动过滤已知无需同步的环境差异字段。
- **安全凭据存储与连接档案**
  - 配置文件仅保存密码引用（`passwordRef`），不保存明文密码：
    - **Windows**：使用系统凭据管理器（Windows Credential Manager / `Advapi32`）。
    - **macOS / Linux**：使用本地 `0600` 权限受限凭据存储。
  - 支持保存常用源/目标连接配置、下拉快速切换，以及**一键互换源/目标端**。
- **同步历史日志**
  - 自动记录每次成功发布同步的源/目标 `DataID`、变更键路径及同步时间，方便回溯核查。

---

## 📂 项目结构

```text
nacos-config-sync-tools/
├── main.go                     # Wails 桌面应用入口，嵌入前端静态资源
├── app.go                      # 桌面端绑定的核心业务方法（配置读写、扫描、合并、发布）
├── wails.json                  # Wails 项目配置
├── internal/
│   ├── credentials/            # 跨平台凭据安全存储（Windows Credential Manager / 本地安全存储）
│   ├── diff/                   # YAML 结构化差异计算、列表对比与保序节点合并
│   ├── matcher/                # DataID 环境后缀识别与目标 DataID 映射规则
│   ├── nacos/                  # Nacos HTTP 客户端（支持 v1/v2/v3 API 与自动回退）
│   ├── rules/                  # 忽略规则匹配与敏感配置（IP/URL/JDBC/Redis/MQ）检测
│   ├── store/                  # 本地设置、连接档案、忽略规则与同步日志持久化
│   └── sync/                   # 同步计划构建器（Planner）与配置发布执行器（Executor）
└── frontend/
    ├── src/                    # 前端界面源码（HTML / CSS / ES Module JS）
    ├── dist/                   # 前端构建产物（由 Go embed 嵌入二进制）
    └── scripts/                # 前端构建、开发预览与渲染测试脚本
```

---

## 🚀 快速开始

### 方式一：直接下载发行版运行

前往本仓库的 [Releases](https://github.com/yeskery/nacos-config-sync-tools/releases) 页面，根据操作系统下载对应压缩包（如 `v1.0.0`）：

- **Windows (x64 / ARM64)**：解压后直接双击运行 `nacos-config-sync-tools.exe`（基于系统内置 WebView2 运行时）。
- **macOS (Apple Silicon / Intel)**：解压后运行 `nacos-config-sync-tools`（首次运行如遇未签名提示，可在「系统设置 -> 隐私与安全性」中允许打开，或执行 `xattr -cr nacos-config-sync-tools`）。
- **Linux (x64)**：需安装 `libgtk-3-0` 与 `libwebkit2gtk-4.0-37`（或 `4.1`），赋予执行权限 `chmod +x nacos-config-sync-tools` 后运行。

### 方式二：从源码运行与测试

**环境要求：**
- Go `1.23+`
- Node.js `18+`（仅修改前端源码后重新构建静态资源时需要）

```bash
# 1. 克隆仓库
git clone https://github.com/yeskery/nacos-config-sync-tools.git
cd nacos-config-sync-tools

# 2. 构建前端静态资源（可选，frontend/dist 已内置最新构建产物）
cd frontend && npm run build && cd ..

# 3. 运行全部单元测试
go test ./...
```

---

## 📖 使用指南

1. **配置源与目标 Nacos**
   - 在左侧边栏分别填写**源 Nacos** 和**目标 Nacos** 的连接信息：
     - **地址**：例如 `http://localhost:8848` 或 `http://nacos.example.com/nacos`
     - **API 版本**：默认推荐 `v3` 或 `auto`（自动兼容 Nacos 2.x / 3.x）
     - **账号 / 密码**：Nacos 登录用户名与密码
     - **Namespace**：命名空间 ID（留空或填 `public` 表示默认公有命名空间）
   - 选择 **源环境**（如 `local` / `dev`）与 **目标环境**（如 `dev` / `test` / `prod`），填写 **Group**（默认 `DEFAULT_GROUP`）。
   - 点击顶部 **「保存连接」** 可将当前源/目标端点保存至本地，后续可通过「已保存配置」下拉框快速切换，或点击 **「⇅ 交换源/目标」** 快速反转同步方向。

2. **扫描配置差异**
   - 点击顶部 **「扫描差异」** 按钮，工具会自动拉取两端配置列表并逐个解析比对 YAML 内容。
   - 扫描结果按状态分类展示：
     - `修改 (changed)`：源端有新增或修改的配置项待同步。
     - `新建 (create)`：目标端尚不存在该配置文件，发布时将在目标端新建。
     - `目标更多 (targetExtra)`：仅目标端比源端多出配置项（默认不覆盖删除目标端特有配置）。
     - `一致 (unchanged)` / `跳过 (skipped)`：无差异或不属于当前源环境后缀的配置。

3. **对比、微调与忽略规则**
   - 点击左侧文件列表中的任一配置，右侧将展示：
     - **环境敏感项警告**：若变更涉及数据库、Redis、MQ、IP/域名等，顶部会展示黄色预警提示。
     - **变更明细与逐项操作**：可针对单个键路径或列表元素点击「同步此项」「保留目标值」或「加入忽略规则」，也可在右侧编辑器直接手动微调合并后的预览 YAML。
   - 切换到 **「忽略规则」** 标签页可查看或移除已添加的忽略规则。

4. **批量发布同步**
   - 在文件列表中勾选需要发布的配置文件（支持按状态筛选与全选）。
   - 确认预览内容无误后，点击顶部 **「发布选中」** 按钮完成同步。
   - 切换到 **「同步日志」** 标签页可查看历史同步记录与变更路径明细。

---

## 📦 多平台编译与打包说明

本项目已完成全平台无 CGO（`CGO_ENABLED=0`）跨平台适配：

- **Windows 平台**：使用 Wails v2 原生桌面运行时（基于 `go-ole` + `go-webview2`，纯 Go 实现无需 CGO），直接打包为无黑框桌面应用。
- **macOS 与 Linux 平台**：内置嵌入式本地服务与 RPC 桥接层（`main_other.go` + `rpc.go`），启动后自动拉起 Chrome/Edge 独立 `--app` 桌面窗口（或系统默认浏览器），支持在 Windows/macOS/Linux 任意环境下直接一键交叉编译全平台二进制包，也支持通过 `-headless` 参数在 Linux 服务器后台运行。

### 一键交叉编译全平台（PowerShell）

```powershell
$env:CGO_ENABLED="0"

# 1. Windows x64 (amd64)
$env:GOOS="windows"; $env:GOARCH="amd64"
go build -tags "desktop,production" -ldflags "-s -w -H windowsgui" -o dist/nacos-config-sync-tools.exe .

# 2. Windows ARM64
$env:GOOS="windows"; $env:GOARCH="arm64"
go build -tags "desktop,production" -ldflags "-s -w -H windowsgui" -o dist/nacos-config-sync-tools-windows-arm64.exe .

# 3. macOS Apple Silicon (M1/M2/M3/M4 arm64)
$env:GOOS="darwin"; $env:GOARCH="arm64"
go build -ldflags "-s -w" -o dist/nacos-config-sync-tools-darwin-arm64 .

# 4. macOS Intel (amd64)
$env:GOOS="darwin"; $env:GOARCH="amd64"
go build -ldflags "-s -w" -o dist/nacos-config-sync-tools-darwin-amd64 .

# 5. Linux x64 (amd64)
$env:GOOS="linux"; $env:GOARCH="amd64"
go build -ldflags "-s -w" -o dist/nacos-config-sync-tools-linux-amd64 .

# 6. Linux ARM64
$env:GOOS="linux"; $env:GOARCH="arm64"
go build -ldflags "-s -w" -o dist/nacos-config-sync-tools-linux-arm64 .
```

---

## 📄 本地配置文件位置

应用运行时的连接档案、忽略规则及同步日志默认保存在系统用户配置目录下：

- **Windows**：`%APPDATA%\nacos-config-sync-tools\settings.json`（密码保存于 Windows 凭据管理器）
- **macOS**：`~/Library/Application Support/nacos-config-sync-tools/settings.json`
- **Linux**：`~/.config/nacos-config-sync-tools/settings.json`
