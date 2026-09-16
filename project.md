# CLICD 项目架构、功能设计与全景接力文档 (Project Handover Documentation)

本文档面向后续 AI 接力开发与架构维护，全面汇总了 **CLICD (LXC/KVM 虚拟化管理面板)** 的系统架构、各模块代码职责、核心技术设计、近期的关键改动与演进记录（涵盖 v1.20 ~ v1.20.6 以及最新 Telegram Bot 集成），并附带现存待办需求与运维指令。

---

## 📌 项目基本信息
- **项目名称**：CLICD (Container & KVM Lifecycle Controller Daemon)
- **当前版本**：`v1.20.6`
- **代码仓库**：[https://github.com/4kercc/CLICD](https://github.com/4kercc/CLICD)
- **测试验证服务器**：`<测试机地址>:<端口>` （凭据单独保管）
- **面板运行地址**：`http://<测试机地址>:<面板端口>/` （凭据单独保管）
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

### 2. 测试机 (`<测试机地址>`) 部署与服务管理
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
