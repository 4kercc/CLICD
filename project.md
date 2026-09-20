# CLICD 项目架构、功能设计与全景接力文档 (Project Handover Documentation)

本文档面向后续 AI 接力开发与架构维护，全面汇总了 **CLICD (LXC/KVM 虚拟化管理面板)** 的系统架构、各模块代码职责、核心技术设计、近期的关键改动与演进记录（涵盖 v1.20 ~ v1.20.8，含 Telegram Bot、预设初始化命令、Windows 装机链路与额外挂载光盘等），并附带现存待办需求与运维指令。

---

## 📌 项目基本信息
- **项目名称**：CLICD (Container & KVM Lifecycle Controller Daemon)
- **当前版本**：`v1.20.8`
- **代码仓库**：[https://github.com/4kercc/CLICD](https://github.com/4kercc/CLICD)
- **测试验证服务器**：部署与验证均在自备测试机上进行（主机地址与账号凭据单独保管，不入库、不写入本文档）
- **面板运行地址**：测试机 `http://<测试机地址>:<面板端口>/`（凭据单独保管）
- **后端技术栈**：Go 1.24+（原生标准库 + Libvirt + LXC + Cgroup v2 + Iptables，无重型框架，纯静态二进制打包）
- **前端技术栈**：React 18 + TypeScript + Vite + Tailwind CSS + Lucide Icons（打包产物嵌入在 `backend/internal/server/web/` 中）

---

## 🏗️ 核心模块代码分工与目录结构

### 1. 后端目录结构 (`backend/`)
- `main.go`：服务入口，解析 `server` 与 `cli` 参数，初始化网络与端口映射，优雅捕获退出信号。
- `internal/config/`：
  - `config.go`：全局数据结构定义（`Container`、`ClicdConfig`、`StoragePool`、`TelegramConfig` 等）与并发读写锁。
  - `store_sqlite.go`：纯 Go 实现的 SQLite 持久化存储引擎（`/root/.clicd/config.db`），支持表结构自动初始化与全量数据原子事务落盘。
  - `nat_network.go`：NAT 子网断言与合法性校验（`IsValidLXCNATIP` 检验 `10.0.3.0/24`，`IsValidKVMNATIP` 检验 `192.168.122.0/24`）。
- `internal/telegram/`：
  - `bot.go`：原生内置 Telegram Bot 核心引擎。采用 **Long Polling 长轮询**（无需公网 Webhook/反代），内置 Chat ID 白名单安全鉴权、指令路由、Inline 键盘菜单交互、宿主机状态卡片、实例开关机/快照/重置密码及全局安全告警主动推送。
- `internal/api/`：
  - `handlers.go`：容器/虚拟机基础 CRUD 接口与单机操作控制器。
  - `runtime.go`：多虚拟化引擎（LXC/KVM）抽象适配层与统一回调网关（解耦 Bot 与底层 Manager）。
  - `taskqueue.go`：全局异步任务队列调度器（支持 `maxConcurrency` 严格节流，防止打快照/批量开机阻塞母鸡 I/O），支持 `batch-action`、`batch-config` 批量配置调整。
  - `routing.go`：路由管理控制器，提供 NAT4 端口范围、公网 IPv4 池、IPv6 前缀、局域网 DHCP 与 **内网 (NAT) IP 分配视图及手动修改接口 (`PUT /api/v1/routing/nat-allocation`)**。
  - `security.go`：轻量安全监控引擎（基于 Conntrack 监测端口扫描、横向扫描、爆破攻击、多周期挖矿判定并自动挂钩 Telegram 告警推送）。
  - `snapshots.go`：快照与全量备份接口，支持单机与批量触发远程存储异地双写。
  - `settings.go`：面板设置接口（任务队列、账号密码、访问来源白名单、SSL 证书、Telegram Bot 设置与测试连通性）。
- `internal/kvm/`：
  - `kvm.go`：Libvirt QEMU/KVM 虚拟机生命周期管理、XML 模板生成、QEMU Guest Agent 交互、fsinfo 真实磁盘读取、在线热扩容、静态 DHCP 绑定 (`virsh net-update`)。
- `internal/lxc/`：
  - `lxc.go`：LXC 容器生命周期管理、cgroup v2 动态限速限额、**硬件 MAC 固化 (`lxc.net.0.hwaddr`)、内网 IP 静态租期同步 (`/etc/lxc/dnsmasq.conf`)**。
  - `portmap.go`：基于 iptables 的 NAT 端口映射与 DNAT 规则同步。
- `internal/server/`：
  - `server.go`：HTTP/HTTPS 路由注册、中间件装配与服务启动器。
  - `embed.go`：将前端静态资源（`web/`）嵌入 Go 二进制文件。

### 2. 前端目录结构 (`frontend/`)
- `src/pages/`：
  - `Containers.tsx`：容器管理列表页（集成批量操作工具栏、状态指示、快速开机/关机/重启/删除）。
  - `ContainerDetail.tsx`：容器/虚拟机详情控制台（实时仪表盘、NAT 端口管理、快照管理、VNC/WebSSH 控制台、Guest Agent 状态）。
  - `Routing.tsx`：路由管理页面（NAT4 端口范围、公网 IPv4 池、IPv6 前缀、局域网 DHCP、**内网 (NAT) IP 分配视图与行内「修改」弹窗**）。
  - `Settings.tsx`：面板系统设置（任务队列并发数、账号设置、两步验证 2FA、**Telegram Bot 管理配置卡片**、访问来源白名单、SSL 证书、登录日志）。
- `src/components/`：
  - `BatchConfigModal.tsx`：批量配置修改弹窗（细粒度复选框控制覆盖字段，支持 vCPU、内存、上下行限速、磁盘 IO 限速、流量上限/重置、到期时间、端口配额、密码重置）。
  - `BatchSnapshotModal.tsx`：批量创建快照确认弹窗（支持选择本地或远程存储池）。
- `src/services/api.ts`：Axios API 封装层，定义所有 TypeScript 数据结构与后端请求方法。

---

## 🚀 近期重要功能演进与技术改动 (v1.20.4 ~ v1.20.6)

### 1. 🤖 原生内置 Telegram Bot 模块 (最新新增)
- **免公网 Webhook**：基于 Go 标准库 `net/http` 原生实现 Telegram 长轮询 (`getUpdates`)，母鸡无需额外域名和 SSL 反代即可直接通信。
- **安全白名单鉴权**：强制校验请求来源 `Chat ID`，非白名单请求直接忽略丢弃。
- **快捷指令菜单自动下发**：服务启动及更新 Token 时自动调用 Telegram 官方 API (`setMyCommands`) 同步注册 `/menu`、`/status`、`/list`、`/batch`、`/web`、`/help` 菜单。
- **交互功能**：
  - `/menu` 或 `/start`：呼出 Inline 键盘主控制菜单；
  - `/status`：返回母鸡 CPU、内存、负载、磁盘、网络速率实时卡片；
  - `/list`：分页列出所有小鸡（带 🟢/🔴 状态），点击进入单机详情面板进行开机、关机（二次确认）、重启（二次确认）、打快照、重置密码（代码块私密回显）、查看内网 IP 与端口映射；
  - `⚡ 批量控制中心`：支持一键向后台任务队列推送批量电源操作、批量应用规格配置、批量创建实例及批量回滚快照；
  - `🛡️ Web 访问安全开关`：支持一键开启/关闭 Web 入口，封锁时对外直接返回 404 伪装阻断；
  - **敏感信息保护**：实例详情中密码采用 Telegram 剧透防窥阴影格式（`||password||`），点击解开；NAT 端口映射自动解析显示宿主机真实公网 IPv4。
- **主动告警推送**：安全引擎触发挖矿告警、暴力破解或流量超标自动关机时，自动调用 `telegram.Global().SendSecurityAlert` / `SendEventNotification` 向管理员 TG 推送结构化报警卡片。
- **前端配置管理与脱敏**：在 `Settings.tsx` 中新增「Telegram Bot」专区，API 接口返回脱敏掩码（`895065...PWaQ`），支持配置 Bot Token、Chat ID 白名单、推送开关及「发送测试消息」连通性测试。
- **涉及文件**：
  - `backend/internal/telegram/bot.go`：TG Bot 核心生命周期、长轮询、消息发送、指令路由与回调逻辑。
  - `backend/internal/config/config.go`：定义 `TelegramConfig` 与回调桥接函数。
  - `backend/internal/api/runtime.go`：实现 Bot 与 LXC/KVM Manager 之间的解耦回调（开关机、重置密码、批量建机与配置等）。
  - `backend/internal/api/settings.go`：TG 设置的读取（Token 脱敏掩码）、保存与连通性测试。
  - `backend/internal/api/security.go`：安全扫描引擎对接 TG 主动推送。
  - `frontend/src/pages/Settings.tsx`：前端 Telegram 管理卡片 UI。
  - `frontend/src/services/api.ts`：前端 Telegram 设置的 API 接口定义。

### 2. 🛡️ Web 访问入口安全控制与应急自愈 (v1.20.5+)
- **双重控制机制**：
  - **Telegram 远程控制**：在 Bot 中输入 `/web` 或点击菜单一键关闭/开启 Web 访问入口；
  - **本地 CLI 命令行控制**：在宿主机终端输入 `sudo clicd web on` / `sudo clicd web off` / `sudo clicd web toggle` / `sudo clicd web status` 随时启闭与查询状态；
  - **交互菜单控制**：在 `sudo clicd` 交互菜单按 `9` 即可一键切换状态。
- **404 隐身伪装防护**：当 Web 入口关闭时，系统所有页面及 `/api/` 路由均统一返回原生 `404 Not Found`，不暴露任何面板特征或服务信息。
- **登录界面脱敏**：登录页面移除了版本号（`CLICD v1.2.0`）展示，防止外部侦察版本特征。
- **涉及文件**：
  - `backend/internal/config/config.go`：`ClicdConfig` 结构体新增 `WebAccessDisabled bool` 字段。
  - `backend/internal/config/store_sqlite.go`：SQLite `app_meta` 读写新增 `web_access_disabled` 键的持久化存储。
  - `backend/internal/server/access_policy.go`：`panelAccessMiddleware` 拦截中间件检测到关闭时直接调用 `http.NotFound(w, r)`。
  - `backend/main.go`：参数路由支持 `clicd web` 子命令转发至 CLI 控制模块。
  - `backend/internal/cli/web_command.go`：实现 `RunWebCommand` CLI 控制指令（`status/on/off/toggle`）。
  - `backend/internal/cli/cli.go`：更新交互菜单第 `9` 项文字及状态切换逻辑。
  - `backend/internal/telegram/bot.go`：TG Bot 增加 `/web` 指令及 `sendWebAccessMenu` / `executeWebAccessToggle` 开关交互。
  - `frontend/src/pages/Login.tsx`：移除底部的版本号标签。

### 3. 🔀 内网 (NAT) IP 分配视图与手动修改 (v1.20.5)
- **背景**：解决以前用户无法直观查看小鸡内网 IP 分配，以及无法根据需求手动固定内网 IP 的问题。
- **前端支持**：在 `Routing.tsx` 的「内网 (NAT) IP 分配」表格中新增「修改」操作按钮与弹窗，支持合法 IP 校验与重复冲突拦截。
- **后端支持 (`PUT /api/v1/routing/nat-allocation`)**：
  - **LXC 容器**：自动同步更新 `/etc/lxc/dnsmasq.conf` 的 `dhcp-host=<mac>,<ip>` 静态租期，向 dnsmasq 发送 `SIGHUP` 信号热重载，更新容器内 `10-eth0.network` 与 `/etc/network/interfaces`，并在运行状态下调用 `lxc-attach` 刷新客机网络及更新 iptables DNAT 规则。
  - **KVM 虚拟机**：自动调用 `virsh net-update default add ip-dhcp-host` 绑定 libvirt 静态租期并同步更新 iptables 规则。

### 3. 🛡️ LXC 硬件 MAC 固化与 IP 防漂移 (v1.20.5)
- **根因分析**：LXC 默认配置中未指定 `lxc.net.0.hwaddr`，导致每次容器重启底层都会随机生成新 MAC，促使 `dnsmasq` 重新下发新 IP。
- **修复方案**：新增 `ensureLXCMACAddress` 与 `randomLXCMAC`，在容器创建和启动前自动注入固化 MAC 地址至 `/var/lib/lxc/<name>/config` 与数据库，彻底消除重启后内网 IP 变动缺陷。

### 4. 🌐 NAT 子网过滤与防 Docker 网桥干扰 (v1.20.5)
- 强化 `GetContainerIP` 与 `firstIPv4` 探针逻辑，引入 `IsValidLXCNATIP`（`10.0.3.0/24`）与 `IsValidKVMNATIP`（`192.168.122.0/24`），严格过滤排除 Docker 网桥（`172.17.0.1`）等外部网卡的干扰，保证内部 IP 与端口转发的准确性。

### 5. 🛠️ 批量修改配置 & 批量打快照 (v1.20.5)
- **批量修改配置 (`POST /api/v1/batch-config`)**：支持勾选多台实例批量调整硬件配置、网络限速、磁盘限速、流量模式/重置、到期时间、端口/快照配额与批量密码重置，支持字段级细粒度覆盖且运行中实例热应用生效。
- **任务队列节流批量快照 (`TaskSnapshot`)**：多选容器后一键打快照并自动排队入队 `TaskQueue`，严格受 `maxConcurrency` 节流，杜绝母鸡 I/O 阻塞。

### 6. 🚀 容器/虚拟机预设初始化命令 (Init Script) (最新新增 - 2026-09-17)
- **需求背景**：用户在创建单台实例、批量开设或重装实例时，支��配置自定义的「预设初始化命令/脚本 (Init Script)」。实例在首次启动就绪（连网成功）后在后台自动静默执行预设命令（如自动安装 `wget`、`curl`、`lrzsz`、`iftop`、`htop`、`docker` 等常用工具），无需人工登录逐台装包。
- **KVM Linux (Cloud-Init `seed.iso`)**：在 `backend/internal/kvm/kvm.go` 的 `createSeedISO` 中将用户的 `InitScript` 追加至 `#cloud-config` 的 `runcmd` 列表，虚拟机首次开机后自动执行并将日志重定向输出至客机 `/var/log/clicd-init-script.log`。
- **KVM Windows (Unattend ISO)**：在 `backend/internal/kvm/kvm.go` 的 `windowsFirstLogonPowerShell` 中将 `InitScript` 写入 `FirstLogon.ps1` 尾部，并在首次管理员登录时通过 PowerShell 静默执行并记录日志至 `C:\CLICD\init.log`。
- **LXC 容器执行机制**：在 `backend/internal/lxc/lxc.go` 中，在 `EnsureSSH` 及容器网络就绪后，调用 `lxc.ExecuteInitScriptAsync` 在后台异步通过 `lxc-attach` 执行用户预设命令，并将日志记录于容器内 `/var/log/clicd-init-script.log`。
- **数据结构与持久化**：
  - `backend/internal/config/config.go`：`Container` 结构体新增 `InitScript string json:"init_script,omitempty"`。
  - `backend/internal/lxc/lxc.go`：`ContainerConfig` 结构体新增 `InitScript string json:"init_script,omitempty"`。
  - `backend/internal/config/store_sqlite.go`：SQLite `containers` 表及 `tasks` 表支持自动迁移并读写 `init_script` / `cfg_init_script` 字段。
- **任务队列与 API 透传**：
  - `backend/internal/api/handlers.go`：单机创建、批量创建 (`batch-create`) 接口支持解析并透传 `init_script`。
  - `backend/internal/api/taskqueue.go`：重装系统 (`reinstall`) 任务支持提取并透传 `init_script` 至底层 `ReinstallContainer`。
- **前端交互与快捷预设**：
  - `frontend/src/services/api.ts`：更新 TypeScript 接口定义（`Container`、`CreateContainerRequest`、`ReinstallContainerOptions`）。
  - `frontend/src/components/CreateContainerModal.tsx`（创建容器向导）：在镜像配置步中增加「预设初始化命令 (Init Script)」折叠面板，提供等宽代码框与常用预设按钮，并在第 4 步预览清单中展示配置状态。
  - `frontend/src/pages/ContainerDetail.tsx`（重装系统）：重装弹窗中新增「预设初始化命令 (Init Script)」配置与快捷填充按钮。
  - 提供快捷预设：📦 *常用工具包 (`wget/curl/iftop/htop`)*、🐳 *安装 Docker* 与清空功能。
- **实机验证与多行脚本执行优化（测试机）**：
  - **排查修复多行执行异常**：此前若用户多行命令包含 `&&` 换行拼接（如同时点击了工��包和 Docker 安装），直接作为 `sh -c` 字符串执行时触发了 `sh: Syntax error: "&&" unexpected` 语法错误。
  - **脚本执行器封装增强**：在 `backend/internal/lxc/lxc.go` 的 `ExecuteInitScriptAsync` 中改为生成沙箱临时脚本 `/tmp/.clicd_init_script.sh`（带 `chmod +x` 与 `set -e` 自动保护），并优化前端预设填充逻辑采用换行分割拼接，彻底消除多行 Shell 命令的语法解析隐患。
  - **实机全量验证通过**：在测试服务器上创建实例 `ccc-good`，注入「常用工具包 (`wget/curl/lrzsz/iftop/htop/btop/net-tools`) + 官方 Docker Engine 安装脚本」，验证客机内 `/usr/bin/htop`、`/usr/sbin/iftop`、`/usr/bin/wget`、`/usr/bin/curl` 及 `docker-ce` 全部安装就绪，日志记录完整。

### 7. 🔐 Telegram 登录成功提醒 (最新新增 - 2026-09-18)
- **需求背景**：为及时感知控制面板的登录行为，Telegram Bot 新增「登录提醒」能力。**仅在登录成功时推送**，登录失败（密码错误、账号不存在、2FA 校验失败、子用户无可用容器等）一律不推送，避免被爆破尝试刷屏。
- **推送内容**：登录账号（含身份角色：超级管理员 / 子用户 / 子用户快捷链接）、来源 IP、客户端 User-Agent、节点主机名与登录时间，便于第一时间识别异常来源。
- **数据结构**：
  - `backend/internal/config/config.go`：`TelegramConfig` 新增 `NotifyLogins bool` (`json:"notify_logins"`)，随 `app_meta.telegram` JSON 一并持久化，无需改动 SQLite 表结构。
- **推送实现**：
  - `backend/internal/telegram/bot.go`：新增 `SendLoginNotification(username, role, ip, userAgent string)`，复用已配置的 BotToken / AdminChatIDs / ProxyURL 长轮询客户端；推送成功或失败均写入 `[Telegram] Login notification ...` 运行日志，便于排查投递链路。
- **触发点（仅成功分支）**：
  - `backend/internal/api/auth.go`：`HandleLogin` 中通过 bcrypt 与（启用时）2FA TOTP 双重校验后，在 `RecordLoginLog(..., true)` 之后以 goroutine 异步触发 `go telegram.Global().SendLoginNotification(req.Username, "超级管理员", ip, ua)`，不阻塞登录响应。
  - `backend/internal/api/subuser.go`：`HandleSubUserLogin` 与 `HandleSubUserAccessCode` 在密码校验通过且存在可用容器后触发，角色分别标记为「子用户」与「子用户快捷链接」。
- **接口与前端**：
  - `backend/internal/api/settings.go`：`TelegramSettingsRequest/Response` 增加 `notify_logins`，GET 返回当前状态、PUT 保存并 `telegram.Global().Restart()` 即时生效。
  - `frontend/src/services/api.ts`：`TelegramSettings` 接口增加 `notify_logins: boolean`。
  - `frontend/src/pages/Settings.tsx`：Telegram Bot 卡片新增「推送登录成功提醒」复选框（默认开启），并接入 `handleSaveTelegram` / `fetchTelegram` 的读写回填。
- **实机验证（测试机）**：
  - 部署新二进制并启用 `notify_logins` 后，通过接口创建一个临时子用户并完成一次成功登录，服务端日志输出：
    `[Telegram] Login notification pushed: user=user-668ed93b role=子用户 ip=192.168.122.84:62463`，确认消息已成功投递至管理员 TG（无发送失败日志）。
  - 验证结束后已清理临时子用户数据并重启服务，环境恢复原状。

### 8. 💿 KVM Windows ISO 挂载防呆与 `__invalid_image_id__` 启动报错修复 (最新修复 - 2026-09-18)
- **根因分析**：
  - 在生成 Windows 虚拟机的 Libvirt Domain XML 时，此前直接调用了 `ImagePath(c.Template)`；当用户修改了虚拟机模板标识或直接导入外部磁盘镜像时，`ImagePath` 会返回不存在的默认占位符 `/var/lib/clicd/images/kvm/__invalid_image_id__.iso` 并强行作为光驱写入配置，导致 QEMU 启动时检测到文件不存在报错 `Cannot access storage file ... No such file or directory`。
- **修复方案**：
  - 在 `backend/internal/kvm/kvm.go` 的 `ApplyContainerLimits` 与 `ensureDomainDefinition` 中引入 **ISO 存在性安全校验**：
    - 优先采用用户自定义填写的 `c.BootMedia`；
    - 若未指定，则仅当镜像库中存在且物理文件真实存在时才作为光驱挂载；
    - 否则 `winISO` 保持为空字符串，Domain XML 生成器自动跳过生成该虚拟光驱，不再写入任何无效占位符。
  - 支持热挂载/热更新：当虚拟机在运行中调整硬件配置修改 `BootMedia` 时，底层自动调用 `virsh change-media` 对光盘进行 `--insert` / `--update` / `--eject` 动态热插拔。
- **实机验证（测试机）**：
  - 成功为运行中的 Windows 虚拟机 `vm-4`（`jsq-windows`）热挂载 `/var/lib/clicd/images/kvm/custom-kvm-770d5fc03f.iso` 到虚拟光驱 `hdb`，`virsh domblklist vm-4` 确认光驱源已正确更新为自定义 ISO。

### 9. 🪟 创建向导「选 Windows 却装出 Debian 12」根因修复 (最新修复 - 2026-09-20)
- **问题现象**：在创建向导中选择 Windows 镜像，创建出来的实例却仍然是 Debian 12；且自定义 Windows 镜像在网络步骤显示为 SSH 22 而非 RDP 3389。
- **根因（三处叠加缺陷）**：
  1. **前端 Windows 识别错误**：`isWindowsTemplate()` 仅用 `templateID.includes('windows')` 做字符串匹配，而用户自定义镜像的 ID 形如 `custom-kvm-42e957647c`（不含 windows），导致自定义 Windows 镜像被当作 Linux 处理。
  2. **创建向导 OS 选择易被误操作**：「系统模板」是普通下拉框，而下方「子用户可用镜像」是一整片醒目的复选框网格，用户极易把后者当成系统选择器 —— 勾选 Windows 复选框只影响子用户权限，不会改变本次安装的系统，于是仍以默认的 Debian 12 建机。
  3. **Windows 虚拟机引导顺序缺陷（致命）**：新建 Windows 虚拟机使用空磁盘，但 Domain XML 默认只写 `<boot dev='hd'/>`，SeaBIOS 在空盘上直接以 `Boot failed: not a bootable disk / No bootable device` 中止，永远不会回退到安装光盘，导致即使镜像选对也无法进入安装程序。
- **修复方案**：
  - **新增共享工具 `frontend/src/utils/templateKind.ts`**：以 API 返回的 `distro` 字段为准注册 Windows 模板（`registerTemplateKinds`），`isWindowsTemplate` 优先查注册表、再退化为 ID 关键字匹配。`CreateContainerModal.tsx` 与 `ContainerDetail.tsx` 均改为引用该共享实现，删除各自原有的错误字符串匹配。
  - **重做创建向导「镜像选择」步骤**：把「系统模板」下拉框改为**大卡片单选**（标题明确为「要安装的系统（单选，决定本次装出的系统）」，Windows 卡片额外标注 `· Windows`）；「子用户可用镜像」改为虚线框区块并注明「仅控制子用户能看到/重装哪些系统，不影响上面选的安装系统」。
  - **修正 KVM 引导顺序（`backend/internal/kvm/kvm.go`）**：Windows 与 Linux Domain XML 的默认引导项改为 `hd → cdrom` 回退链，`network` 引导时为 `network → cdrom → hd`。空盘时 SeaBIOS 自动回退到安装光盘；系统装好后硬盘可引导则优先走硬盘，光驱中的 ISO 被自动忽略，无需人工弹出。
- **实机验证（测试机）**：
  - 通过浏览器实际驱动面板创建向导：选 KVM 后 Windows 卡片正确识别（网络步骤显示 **RDP: 22015 -> 3389**，vCPU/内存/磁盘自动提升为 2C/2048MB/30GB）；预览清单「系统镜像」正确显示为所选 Windows 镜像。
  - 创建实例并抓取控制台截图，确认 SeaBIOS 走 `hd → cdrom` 回退并成功进入 **Windows Server 2019 安装程序（“安装程序正在启动”）**，系统盘为空盘（`<backingStore/>`）而非 Debian 覆盖层，彻底闭环。
  - **重要提示**：镜像 `custom-kvm-770d5fc03f`（`windows server 2019` / `2019-virto.iso`，2.2GB）经校验 **缺少 El Torito 引导记录（第 17 扇区为终止描述符，且 Boot System ID 为 LINUX）**，属不可引导的数据盘，任何平台都无法用它安装系统；请改用 `custom-kvm-42e957647c`（`cn_windows_server_2019_x64_dvd_4de40f33_virtio_20190225.iso`，5.3GB，第 17 扇区为 `EL TORITO SPECIFICATION`）等可引导安装镜像。
  - 验证完成后已删除测试实例 `win-verify`、`win2019-check`，服务器环境恢复原状。

### 10. ⏱️ 「一直卡在关机中 / 无法删除」任务假死修复 (最新修复 - 2026-09-20)
- **问题现象**：实例停在「关机中…」按钮状态长时间不结束，期间删除等操作全部排队不动，看起来像卡死无法移除。
- **根因（两处长等待，被误判为死锁）**：
  1. **KVM 关机固定等待 45 秒**：`StopContainer` 先发 ACPI 关机再轮询 45 秒。对于系统盘还是空的 Windows 实例（正在跑安装程序 / 从未装系统），客户机根本不响应 ACPI，于是必然耗满 45 秒才强制断电。
  2. **KVM 开机最长等待 180 秒获取内网 IP**：`StartContainer` 以 `IsWindowsImage()` 判断是否等待 IP；而运维在界面上手动改过系统标签的实例（如把模板标签改成 `windows`）无法被 `FindImage` 解析，被误判为 Linux，于是空盘实例死等 180 秒拿不到 IP 才失败。
  3. 由于任务队列按实例串行（`activeTargets`），上述长任务期间排队在其后的关机/删除任务只能等待，前端按钮呈现「关机中…」不结束。
- **修复方案**：
  - **自适应关机等待窗口**：新增 `stopGracefulWindow()` 与 `systemDiskAllocatedBytes()`，用 `virsh domblkinfo` 读取系统盘真实分配量（运行中也安全）。当模板判定为 Windows 且系统盘分配量 < 512 MiB（无系统、无可丢数据）时，优雅关机窗口由 45 秒缩短为 5 秒；一旦盘上已有实际数据（含安装程序写入）或为 Linux 云镜像覆盖盘，仍保留完整 45 秒优雅窗口，确保不丢数据、不损坏文件系统。
  - **放宽 Windows 标签识别**：新增 `looksLikeWindowsTemplate()`，在 `IsWindowsImage()` 基础上兼容运维手动改写的模板标签（含 `windows` 关键字），开机路径不再对空盘 Windows 实例死等 180 秒 IP。
  - **前端不再残留假死页面**：`ContainerDetail` 的 `fetchContainer` 遇到 404（实例已被删除）时提示并自动返回列表页，不再长时间渲染旧快照；操作失败时改为直接展示服务端返回的中文原因，并对 409（已有任务排队）给出明确的「请等待当前任务结束」提示。
- **实机验证（测试机）**：
  - 修复前实测：新建 Windows 实例关机耗时 **48 秒**；运维改标签的 `1111` 实例开机任务卡住并在其后堆积挂起的关机任务。
  - 修复后实测：同样的关机耗时 **6 秒**；`1111` 实例开机 20 秒、关机 10 秒全部正常结束，堆积任务清空。
  - 全链路复测（新建 → 关机 → 删除）：关机 6 秒、删除 2 秒，实例彻底移除；`virsh domblkinfo` 校验阈值判定正确（已装 Linux 28534 MiB / 已装 Windows 35432 MiB → 走 45 秒优雅窗口；空盘 36 MiB → 走 5 秒快速断电）。
  - 验证实例已删除，服务器仅保留原有 10 个实例。

### 11. 📄 容器列表分页与批量勾选体验优化 (最新优化 - 2026-09-20)
- **需求背景**：容器列表每页只支持 10/20/50 且默认 10，批量运维时可见与可勾选的实例太少。
- **改动内容（`frontend/src/pages/Containers.tsx`）**：
  - **默认每页 20 条**，可选项扩展为 **10 / 20 / 50 / 100**，满足「一页尽量多看、好批量勾选」的诉求。
  - **每页数量本地记忆**：选择结果写入 `localStorage`（键 `clicd_containers_page_size`），刷新或再次进入页面后保持上次选择（如 100），不必每次重设。
  - **新增「选择本页」按钮**：只勾选当前分页显示的实例，配合表头复选框（跨分页作用于全部筛选结果）覆盖两种批量场景；表头复选框补充提示文案说明其作用范围。
  - **新增「取消选择」按钮**：选中若干实例后工具条左侧一键清空勾选，避免逐台取消。
- **国际化**：为新增文案（`取消选择` / 表头提示）补充 `utils/i18n.ts` 词条，英文界面下正常显示。
- **实机验证（测试机）**：浏览器实测确认默认选中 20、下拉含 10/20/50/100、选择 100 后刷新仍为 100、「取消选择」可清空勾选。
- **后续调整**：「选择本页」按钮经实际使用反馈价值有限，已移除（分页栏保留 `首页/上一页/下一页/末页`，批量勾选仍可通过表头复选框跨分页全选 + 「取消选择」清空）。

### 12. 🧩 第三方 WinPE / WePE 镜像无法添加修复 (最新修复 - 2026-09-20)
- **问题现象**：在「镜像管理 → 第三方镜像」中选择 **WinPE / WePE** 模板并点击 Add and Download，弹窗底部报错 `unsupported unattended installation template`，镜像无法注册。
- **根因分析**：后端 `backend/internal/api/images.go` 的 `handleCustomKVMImageCreate` 中，`switch req.Provisioner` 只处理了 `linux-cloud-init` / `windows-10` / `windows-11` 三种无人值守模板，**遗漏了早已在 `config.KVMProvisionerWindowsPE`（`windows-pe`）中定义、前端也已提供入口的 WinPE 类型**，于是落入 `default` 分支直接返回该错误。下载校验（`validateWindowsISO` 的 `isPE` 分支）与 PE 识别（`Image.IsWindowsPE()`）本就已支持，因此仅需补齐注册分支。
- **修复方案（`backend/internal/api/images.go`）**：新增 `case config.KVMProvisionerWindowsPE`：
  - 校验架构必须为 `amd64`（WinPE 引导镜像仅支持 x86_64）；
  - 校验 `distro` 字符集（沿用 `customImageFieldPattern`），为空时归一为 `wepe`；
  - 统一将 `release` 归一为 `pe`，保证 `IsWindowsPE()` 判定稳定；
  - WinPE 属纯引导维护镜像，不生成无人值守应答文件（注释已注明）。
- **实机验证（测试机）**：用弹窗实际提交的完整载荷（`provisioner=windows-pe`、`distro=wepe`、`release=pe`、`arch=amd64`）调用接口，注册由原先的 400 报错变为 **201 成功**（`custom-kvm-*`），并确认已正确落盘到 SQLite `app_meta.custom_kvm_images`；重复 URL 会按既有规则返回 409 去重提示。验证用的临时镜像条目已删除，镜像列表恢复原状。

### 13. 🌐 英文界面残留中文清理与 i18n 覆盖补全 (最新修复 - 2026-09-20)
- **问题现象**：切换到英文后面板仍夹杂中文，且存在「半翻译」混合串，例如 `Disk总线`、`Network InterfacesDriver`、`Page 台Container`、`同步Failed`、`已Config`。
- **根因分析**：
  1. `utils/i18n.ts` 的 `exact` 词典缺少大量界面文案词条（新增功能时只写了中文文案，未同步补词条）。
  2. 匹配策略为「整串精确命中 → 正则替换 → 子串替换」，当长句只命中其中一小段子串时，就会产出半中半英的混合文本。
  3. 含数字插值的模板串（如 ``第 ${n} 台容器``、``轻量级 COW 增量快照，共 ${n} 个``）无法用整串词条覆盖，必须用正则规则处理；而既有的贪婪规则 `[/每\s*(.+)$/, 'Every $1']` 会先改写整串，导致后续精确词条失效（例如 `每页数量` 被改写成 `Every 页数量`）。
- **修复方案**：
  - **新增约 260 条中英对照词条**，覆盖容器详情（引导/硬件、Guest Agent、光驱挂载、导入导出、快照/备份同步）、面板设置（Telegram Bot、2FA）、存储管理、批量配置、快照、路由、镜像管理与登录页。
  - **新增 10 条正则替换规则**处理数字插值模板：`第 N 台容器` → `Container #N`、`第 N 条映射…`、`轻量级 COW 增量快照，共 N 个`、`全量压缩独立归档备份，共 N 个`、`确认彻底删除容器 X 的全量备份文件吗？`、`已成功将 N 个快照任务加入任务队列…` 等。
  - **消除会产生混合翻译的源文案**：把 ``批量端口冲突：X 同时被 Y 和第 N 台容器使用`` 简化为 ``批量端口冲突：X 已被 Y 占用``；把创建向导中带嵌套 `<strong>` 的子用户提示改为单一纯文本，使整句可被词条整体命中。
  - **新增 4 个可复用的自检脚本**（`frontend/scripts/`）：`i18n-audit.mjs`（扫描源码中文串并模拟翻译，列出仍含中文的文案）、`i18n-check.mjs`（用真实运行时字符串做端到端断言，防回归）、`i18n-coverage.mjs`（校验指定文本清单的覆盖率）、`i18n-duplicates.mjs`（检测并清理 `exact` 词典重复键，避免 TS1117）。
- **验证结果**：
  - `i18n-check.mjs`：**39/39** 条真实运行时字符串全部翻译干净（含数字插值形态）；
  - `i18n-coverage.mjs`：对浏览器实测抓取的 **65/65** 条残留中文全部覆盖；
  - `i18n-audit.mjs`：源码仅剩 6 处「扫描器剥离 `${...}` 插值后产生的碎片」，其真实运行时形态已由正则覆盖；
  - `i18n-duplicates.mjs`：重复键 **0**（清理 12 处，保留原有措辞以免影响既有页面）。
- **重要排查提示**：英文模式的翻译由 `AutoTranslate` 组件在 `requestAnimationFrame` 回调中批量应用，**标签页处于后台/隐藏时浏览器不会触发 rAF**，此时实时抓取页面会看到未翻译文本；判断覆盖率请以可见标签页或上述静态校验脚本为准。

### 14. 💿 额外挂载光盘（独立光驱）+ 服务器镜像选择器 (最新改进 - 2026-09-20)
- **需求背景**：
  1. 「自定义挂载 ISO 路径」会**整体替换**镜像自带的光盘。对于 `firepe` 这类 PE 镜像，用户期望的是「先由自带 PE 盘启动进入 PE，再从另一块光驱读 Windows 安装盘」，而不是被自定义 ISO 抢占启动盘。
  2. 该输入框要求手填宿主机绝对路径，容易写错。
- **改动内容**：
  - **新增独立「额外挂载光盘」字段 (`ExtraISO` / `extra_iso`)**：
    - `backend/internal/config/config.go` 新增 `Container.ExtraISO`，`store_sqlite.go` 增加 `containers.extra_iso` 列与自动迁移、读写与占位符同步。
    - `backend/internal/lxc/lxc.go` 的 `ContainerConfig` 增加 `ExtraISO`（创建/重装透传）。
    - `backend/internal/kvm/kvm.go`：新增 `extraCDROMXML()` 与保留槽位常量 `extraCDROMDevice = "sdc"`，Windows/Linux 两个 Domain XML 生成器均在**独立 SATA 光驱**上挂载该 ISO，镜像自带光盘（winISO / seed.iso）完全不受影响；两个生成器同时显式声明 `<controller type='sata' index='0'/>`，保证该光驱可被热插拔。
    - **热插拔**：`syncExtraISO()` 在实例运行中即时生效——若运行中的域还没有该光驱，先 `virsh attach-device` 挂上（libvirt 要求附带介质，因此设备 XML 直接带 `<source>`），之后变更走 `change-media --insert/--update`，清空则 `--eject`；旧实例（域内尚无 SATA 控制器）无法热附加时会打印明确提示并在下次开机自动生效。
    - `applyContainerLimits` 中原先"把 BootMedia 热插到 hdb"的逻辑已移除——那正是抢占启动盘的元凶。
  - **新增服务器端镜像选择接口 `GET /api/kvm-media`**（`/api` 与 `/api/v1` 双前缀）：`kvm.ListAttachableMedia()` 汇总「已下载的注册镜像」与「镜像缓存目录内其它 `.iso`」，返回 `path / name / kind / source / size_bytes`，路径范围与 `boot_media`/`extra_iso` 的白名单一致，不会暴露任意宿主机文件。
  - **前端改为下拉选择**（`ContainerDetail.tsx` 引导与硬件弹窗）：
    - 「启动光盘覆盖 (留空为使用镜像自带光盘)」——默认使用镜像自带光盘；
    - 「额外挂载光盘 (独立光驱，不影响启动光盘)」——默认不挂载；
    - 两者共用服务器镜像列表，并提供「自定义路径…」项回退到手填；`mediaSelection()` 保证已保存但不在列表中的历史路径不会被静默清空。
  - 补充上述新文案的 i18n 词条；同时修正 4 个 i18n 自检脚本解析器只识别单引号值的问题（新增词条使用双引号值时会漏解析）。
- **实机验证（测试机）**：
  - 新建带额外光盘的实例，`virsh domblklist` 显示：`hdb` = 镜像自带 PE 盘（`custom-kvm-c836cfa104.iso`）、`sdc` = 额外 Windows 安装盘（`custom-kvm-42e957647c.iso`）；
  - **运行时切换额外光盘**：`sdc` 由 Windows 2019 镜像换成另一 ISO，**`hdb` 保持不动**；
  - **运行时清空额外光盘**：`sdc` 变为空托盘，`hdb` 依旧不动；
  - `GET /api/kvm-media` 正确返回 5 项可选介质（含用户新加的 `firepe`），前端弹窗两个选择器均正确渲染并默认选中预期项；
  - 验证实例已删除，服务器仅保留原有 10 个实例。

---

## 📝 AI 接力开发与修改记录规范 (Development Guidelines for AI Assistants)
后续所有 AI 助手在接力开发本项目时，必须严格遵守以下规范：
1. **持续同步 `project.md`**：完成任何代码修改、架构调整或需求上线后，必须在 `project.md` 中以清晰的小节记录修改背景、改动文件、技术细节以及实机验证状态。
2. **遵守部署与发布要求**：用户未明确要求发布前，一律仅在本地构建并在测试服务器上验证，严禁擅自直接推送到 GitHub 远程仓库。
3. **跨平台编译与嵌入规范**：修改前端代码后需先执行 `npm run build`，并将 `frontend/dist/*` 同步复制至 `web/` 与 `backend/internal/server/web/` 后，再使用 Go 交叉编译出 Linux AMD64 二进制。
4. **禁止写入任何环境凭据（强制）**：本文档与仓库内所有文件**不得**记录测试机/生产机的 IP、端口、SSH 用户名、面板账号或任何密码。描述验证过程时写成「测试机」「面板」即可，地址与凭据一律单独保管。历史提交中曾出现过的测试机地址与账号已通过 `git filter-repo` 全量重写并使用强制推送清除，重写后请以远端历史为准（旧的本地克隆需重新拉取）。

---

## 🎯 现存待办需求与已完成状态 (Next Steps & Completed Status)

针对 **Telegram Bot 批量控制中心** 的深度增强需求已全部完成并闭环对接：

1. ✅ **Telegram 菜单升级**：将原有的「⚡ 批量电源控制」正式命名升级为「⚡ 批量控制中心」。
2. ✅ **新增 4 个批量子功能菜单与交互流程**：
   - **📷 批量添加快照 (`batch:confirm:snapshot`)**：一键为所有实例创建快照并推送到后台任务队列排队。
   - **⚙️ 批量调整配置 (`batch:menu:config`)**：在 TG 中提供快捷配置模板选择（性能型 2C2G/标准型 1C1G/轻量型/统一限速100M）并热应用至所有实例。
   - **⏪ 批量恢复快照 (`batch:ask:restore_snap`)**：带二次确认机制，一键将所有存在快照的实例快速回滚至各自最新的可用快照。
   - **🚀 批量开设虚拟机/容器 (`batch:menu:create`)**：在 TG 中提供 3/5 台 LXC 容器与 2 台 KVM 等预设模版，自动规划 NAT 端口并推送后台队列并发创建。

---

## 🛠️ 运维、编译与部署指令

### 1. 本地代码编译与打包（在宿主开发机上）
```bash
# 1. 前端构建
cd frontend
npm run build

# 2. 将前端产物同步至 Go embed 目录
cd ..
rm -rf backend/internal/server/web/*
cp -r frontend/dist/* backend/internal/server/web/
touch backend/internal/server/web/.gitkeep

# 3. 本地 Linux 交叉编译（如需直接生成 Linux 二进制）
cd backend
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o clicd-linux-amd64 main.go
```

### 2. 测试机部署与服务管理
```bash
# 查看服务状态
systemctl status clicd --no-pager

# 查看实时日志
journalctl -u clicd -f -n 50

# 重启面板服务
systemctl restart clicd

# 停止服务
systemctl stop clicd
```

### 3. 测试机上的关键文件路径
- **二进制文件**：`/usr/local/bin/clicd`
- **数据库路径**：`/root/.clicd/config.db` (SQLite 数据库)
- **LXC 容器目录**：`/var/lib/lxc/` (软链接至 `/var/lib/clicd/lxc/`)
- **LXC DHCP 静态租期文件**：`/etc/lxc/dnsmasq.conf`
- **LXC 网桥配置**：`/etc/default/lxc-net`
- **KVM 虚拟机数据目录**：`/var/lib/clicd/kvm/`
