<p align="center">
  <img src="frontend/public/favicon.svg" width="96" alt="CLICD">
</p>

<h1 align="center">CLICD <sub></sub></h1>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square&logo=go&logoColor=white">
  <img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?style=flat-square&logo=react&logoColor=111111">
  <img alt="TypeScript" src="https://img.shields.io/badge/TypeScript-5-3178C6?style=flat-square&logo=typescript&logoColor=white">
  <img alt="Vite" src="https://img.shields.io/badge/Vite-5-646CFF?style=flat-square&logo=vite&logoColor=white">
  <img alt="Tailwind CSS" src="https://img.shields.io/badge/Tailwind_CSS-3-06B6D4?style=flat-square&logo=tailwindcss&logoColor=white">
  <img alt="LXC" src="https://img.shields.io/badge/LXC-Supported-111111?style=flat-square">
  <img alt="KVM" src="https://img.shields.io/badge/KVM-Supported-EE0000?style=flat-square">
</p>

<p align="center">
  <img alt="WebSSH" src="https://img.shields.io/badge/WebSSH-Built--in-009688?style=flat-square">
  <img alt="VNC" src="https://img.shields.io/badge/VNC-Supported-7B1FA2?style=flat-square">
  <img alt="IPv6" src="https://img.shields.io/badge/IPv6-Native-1976D2?style=flat-square">
  <img alt="NAT" src="https://img.shields.io/badge/NAT-Port_Forwarding-FF9800?style=flat-square">
  <img alt="REST API" src="https://img.shields.io/badge/API-REST-4CAF50?style=flat-square">
  <img alt="Multi User" src="https://img.shields.io/badge/Multi_User-Supported-8E24AA?style=flat-square">
  <img alt="Traffic Control" src="https://img.shields.io/badge/Traffic-Control-795548?style=flat-square">
  <img alt="Security Alert" src="https://img.shields.io/badge/Security-Alert-orange?style=flat-square">
  <img alt="CLI" src="https://img.shields.io/badge/CLI-Mode-424242?style=flat-square">
  <img alt="TLS" src="https://img.shields.io/badge/TLS-Let's_Encrypt-003A70?style=flat-square&logo=letsencrypt&logoColor=white">
</p>

CLICD is a lightweight virtualization management panel for LXC and KVM. It combines a web console, CLI tools, REST API, NAT/IPv6 networking, WebSSH/WebVNC access, resource quotas, traffic limits, snapshots, delegated sub-user access, and security alerts into a single deployable service.

CLICD 是一个面向 LXC/KVM 的轻量虚拟化管理面板，集成 Web 控制台、CLI、REST API、NAT/IPv6 网络、WebSSH/WebVNC、资源配额、流量限制、快照、子用户授权和安全告警能力，适合 VPS 商家、实验室、开发者自建虚拟化节点以及需要批量开通容器的场景。

![alt text](/img/image-1.png)

## Installation / 安装

One-click Install / 一键安装：

```bash
curl -fsSL https://raw.githubusercontent.com/4kercc/CLICD/main/install.sh | sudo sh
```

One-click Uninstall / 一键卸载：

```bash
curl -fsSL https://raw.githubusercontent.com/4kercc/CLICD/main/install.sh | sudo sh -s -- uninstall
```

---

## 🚀 Recent Enhancements & Enterprise Features / 最新功能特性与优化说明

为了满足生产环境、PVE 虚拟机平滑迁移、大容量 Windows 实例高效运行等需求，本分支进行了以下核心增强（方便后续向原仓库发起 Pull Request）：

### 1. 🔀 KVM 引导管理与灵活硬件驱动选型 (Boot & Hardware Flexibility)
- **第一启动项动态控制 (Boot Order)**：支持自由切换 **硬盘优先 (Hard Disk)**、**光盘/ISO 优先 (CD-ROM)** 或 **网络 PXE 引导**，彻底解决了 Windows/PE 重启后再次跌入光盘安装界面的问题。
- **虚拟硬件模型适配**：
  - **网卡驱动 (NIC Model)**：支持切换为高性能 `VirtIO`（Linux原生、Windows需驱动）、`Intel e1000e`（免驱千兆兼容）或 `RTL8139`。
  - **磁盘总线 (Disk Bus)**：支持切换为 `VirtIO Block (vda)`、`SATA AHCI (sda)`、`IDE (hda)`。

### 2. 🛡️ 快照 (Snapshot) 与 备份 (Backup) 架构分离 + 零停机在线热快照
- **零停机在线热快照 (Live Snapshot)**：结合 QEMU Guest Agent `fsfreeze` 静默冻结与 COW 增量捕获，**打快照无需关机**，1秒内极速完成。
- **自动分层 COW 轻量化**：自动将单体大镜像转换为 `Base (只读基盘) + Overlay (轻量增量层)` 架构，单次快照体积从数十 GB 暴降至 **几十 KB ~ 几十 MB**，彻底避免撑爆宿主机硬盘。
- **独立的 Full Backup 全量备份体系**：新增独立接口与数据表，备份时执行全量合并与 `qcow2` 压缩打包归档，用于长期异地容灾与跨机还原。

### 3. 📈 在线与离线磁盘动态扩容 (Live Disk Resize)
- 支持在面板一键调整 KVM 磁盘容量。
- **在线扩容**：虚拟机开机状态下调用 `virsh blockresize`，并由 Guest Agent 自动触发内部系统文件系统无缝扩展（Windows C 盘自动 extend，Linux 自动 growpart / resize2fs）。
- **离线扩容**：关机状态下调用 `qemu-img resize` 安全扩容。

### 4. 🔄 外部磁盘镜像导入与 PVE/KVM 迁移向导 (Disk Import & Migration)
- 支持直接输入宿主机外部镜像路径（如 PVE 导出的 `vm-301-disk-0.qcow2` 或 raw/vmdk 镜像）。
- 支持一键转为 Base 只读基盘并自动挂载增量层，实现从 PVE 到 CLICD 的极速平滑迁移。

### 5. 💽 Windows PE / WePE 自定义维护镜像支持
- 放宽第三方 Windows 维护镜像的体积限制（支持 50MB~800MB 的 WinPE / WePE / FirPE 镜像）。
- 修复了下载体积较小的 PE 镜像被误判为“文件不完整”的问题。

### 6. 🔐 管理员两步验证 (2FA / TOTP)
- 面板内置标准 RFC 6238 TOTP 双因素认证，支持 Google Authenticator、Microsoft Authenticator、1Password 等身份验证器扫码绑定。
- 开启后登录强制进行 6 位动态验证码校验，大幅提升公网管理面板的防护等级。

### 7. 🌐 NAT 规则双栈一体化管理 (Dual-Stack NAT & IPv6 Auto-Sync)
- 统一 NAT 规则卡片体系，创建/编辑 IPv4 NAT 端口映射时支持一键自动同步生成/放行同端口 IPv6 入站防火墙规则。
- 默认开启一键双栈放行，也可关闭后在防火墙面板中进行独立精细化管理。

### 8. 🔍 挖矿智能深度识别与降误报机制 (Intelligent Cryptomining Detection - v1.20.1)
- 摒弃以往基于 Stratum/常见端口单次命中的简单规则，引入**多周期长连接生命周期追踪（Longevity & Heartbeat Tracking）**。
- 结合 TCP `ESTABLISHED` 持续状态、并发模式、矿池威胁情报与置信度评分（Confidence Score），彻底杜绝 Node.js/开发测试环境/通用代理服务的误报。

### 9. ☁️ 多协议远程存储支持与异地双写灾备 (Remote Storage Pools & Offsite Disaster Recovery - v1.20.1)
- **多协议外部存储驱动**：原生支持添加 **SFTP**、**WebDAV**、**MinIO / AWS S3** 外部存储池，支持面板一键连通性测试。
- **快照/备份自动异地双写**：在存储池中勾选“启用快照/备份同步”后，创建快照或全量备份时后台自动异步双写上传至远端存储。
- **本地 / 远程多源灵活恢复**：恢复快照或全量备份时，若存在远端副本，支持一键选择“从本地极速还原”或“从远程存储拉取还原”，保障极端情况下的数据安全性。

### 10. ⚡ 远程同步/恢复实时进度追踪与面板体验升级 (Live Transfer Progress & UI Polish - v1.20.2)
- **传输实时进度条**：快照/备份上传至远端存储或从远程拉取还原时，提供毫秒级**实时传输进度条、当前文件、传输速率 (MB/s) 及已传输/总容量统计**。
- **Guest Agent 协同指南升级**：集成 Windows（驱动光盘一键挂载）与 Linux（全发行版一键脚本）双模式自主切换 Tab，指导用户按需安装。
- **容器详情页操作体验优化**：
  - 支持**双击主机名**快速重命名，便于多服务器辨识与管理。
  - 支持**双击系统标签**动态修改系统模板标识（纠正图标与连接协议）。
  - **醒目红色删除按钮**移至操作栏末尾，并引入**二次防误触危险确认弹窗**，彻底杜绝误删风险。

### 11. 🛡️ 虚拟化安全加固与稳定性防卡死重构 (Security Hardening & Stability Overhaul - v1.20.3)
- **安全修复（严重）**：修复「导入外部磁盘」接口可被越权利用读取宿主机任意文件的漏洞 —— 源镜像路径现在强制限制在**存储池 / KVM 数据目录 / 镜像缓存目录白名单内**（含符号链接解析），且该功能收紧为**仅管理员可用**。
- **性能修复（高）**：快照/备份的全局互斥锁收窄为仅保护配置登记与轮转删除两个瞬时阶段，长磁盘 I/O 改由单机维度互斥锁保护 —— **一台虚拟机的全量备份不再阻塞其他虚拟机的快照/备份操作**。
- **数据一致性修复（高）**：在线热快照全面防撕裂：
  - 有 Guest Agent 时冻结整个拷贝窗口（应用一致性）；
  - 无 Agent 时自动执行 `virsh snapshot-create-as --disk-only --atomic` 原子外部快照，拷贝安静后备盘后 `blockcommit --active --pivot` 合并回原盘；
  - 拷贝统一使用 `qemu-img convert -U` 共享锁读取，杜绝拷贝正在写入的裸文件。
- **稳定性修复（中）**：全部 `virsh` 热路径调用（domstate / domifaddr / domstats / guest-ping / guest-exec 等）统一加 5–30 秒硬超时，libvirtd 僵死时面板不再整体冻结。
- **其他加固**：服务启动时自动解冻遗留的冻结虚拟机；LXC PID 拼接前强制数字校验（防 shell 注入）；主机名/系统标签统一长度与控制字符校验；优雅关机等待延长至 45 秒，避免强制断电导致 Windows 蓝屏或文件系统损坏。

### 12. ⚡ 批量运维体系 & 内网 IP 固化防漂移 (Batch Operations & Static IP Lock - v1.20.5)
- **批量调整配置 (Batch Config Modification)**：支持勾选多台容器/虚拟机一键批量调整计算资源（vCPU、内存）、网络上下行限速、磁盘读写限速、月度流量配额模式与上限、到期时间、NAT 端口配额、快照配额，并支持批量生成独立随机密码或统一重置密码。支持字段级精准勾选覆盖，运行中实例即时生效。
- **批量创建快照 (Batch Snapshot Creation)**：支持多选实例后选择本地或远程目标存储池一键批量打快照；通过异步任务队列机制调度执行并严格限制并发度，杜绝批量打快照打崩宿主机磁盘 I/O。
- **LXC 容器 MAC 地址持久化与内网 IP 永久固化**：修复 LXC 容器每次重启随机生成 MAC 导致 `dnsmasq` 动态分配新内网 IP 漂移的缺陷。在容器创建和启动生命周期中自动生成并固化 `lxc.net.0.hwaddr`，确保容器重启后内网 IP 绝对固定。
- **KVM/LXC NAT 内网子网精准过滤**：完善 IP 探针过滤规则，排除 Docker 桥接网卡（`172.17.0.1`）及其他虚拟网卡对 KVM/LXC 真实内网 NAT IP（`192.168.122.0/24` / `10.0.3.0/24`）的干扰，确保 iptables DNAT 端口映射及内网通信绝对可靠。
- **路由管理内网 NAT 分配可视化**：在路由管理页面中新增容器与虚拟机的内网 IP 分配状态视图，直观展示各实例内网 IP、网桥与绑定状态。

### 13. 🤖 原生内置 Telegram Bot 控制与安全应急防御 (Telegram Bot Integration & Web Guard - v1.20.6)
- **免公网 Webhook 极速直连**：基于 Go 标准库实现 Telegram API 长轮询 (`getUpdates`) 与多线程事件派发，宿主机无需配置域名与反向代理，处于内网或 NAT 环境亦可稳定控制。
- **严格 Chat ID 白名单鉴权**：强制校验请求来源 `Chat ID`，未授权用户的私聊与指令交互直接忽略丢弃。
- **快捷指令与菜单系统**：
  - `/menu` / `/start`：呼出 Inline 键盘主控制台，支持指令自动注册 (`setMyCommands`)；
  - `/status`：秒级返回宿主机 CPU 仪表盘、内存占用、磁盘空间、网络吞吐与实例健康概览；
  - `/list`：分页列出实例，支持单机开机/关机（带二次确认）、重启（带二次确认）、创建快照、重置密码（剧透防窥代码块）、查看内网 IP 与真实公网端口映射；
  - `⚡ 批量控制中心`：支持一键批量电源控制、批量应用预设规格配置、批量创建实例及批量回滚最新快照；
  - `🛡️ Web 访问安全开关`：支持一键关闭/开启 Web 访问入口；Web 入口关闭时对外直接返回原生 `404 Not Found` 隐身伪装阻断。
- **主动安全威胁预警**：安全扫描引擎检测到挖矿特征、端口暴力破解、异常扫描或流量超标自动停机时，自动向管理员 Telegram 实时推送结构化告警卡片。
- **前端配置管理面板**：在「面板设置」新增「Telegram Bot」专区，支持 Token/Chat ID 管理、推送开关设置与即时连通性测试。

### 14. 🚀 预设初始化命令、登录提醒与 Windows 装机链路修复 (Init Script / Login Alert / Windows Install Fix - v1.20.7)
- **🚀 预设初始化命令 (Init Script)**：创建、批量开设与重装实例时均可配置自定义初始化脚本，实例首次启动并连网就绪后在后台静默自动执行（如批量预装 `wget`/`curl`/`lrzsz`/`iftop`/`htop`/`btop`/Docker 等常用组件），无需逐台登录装机。前端提供「常用工具包」「安装 Docker」一键预设。
  - **KVM Linux**：注入 Cloud-Init `#cloud-config` 的 `runcmd`，日志落盘客机 `/var/log/clicd-init-script.log`；
  - **KVM Windows**：注入无人值守应答 `FirstLogon.ps1`，首次登录自动执行，日志落盘 `C:\CLICD\init.log`；
  - **LXC 容器**：网络与 SSH 就绪后由 `lxc-attach` 在沙箱临时脚本中异步执行，规避多行 `&&` 命令的 Shell 语法解析问题，日志落盘 `/var/log/clicd-init-script.log`。
- **🔐 Telegram 登录成功提醒**：新增 `notify_logins` 开关（面板设置 → Telegram Bot 可配置），**仅在登录成功时推送**账号（区分超级管理员 / 子用户 / 子用户快捷链接）、来源 IP、客户端 UA 与登录时间；登录失败（密码错误、2FA 校验失败、无可用容器等）一律不推送，避免被爆破尝试刷屏。
- **🪟 创建向导与 Windows 装机链路修复（重要）**：
  - **修复「选 Windows 却装出 Debian」**：原先「系统模板」是不起眼的下拉框，下方醒目的「子用户可用镜像」复选框网格易被误当成系统选择器（勾选它只影响子用户权限）。现已将系统选择改为**大卡片单选**并明确标注标题，子用户镜像区改为虚线框并注明「不影响上面选的安装系统」。
  - **修复自定义 Windows 镜像识别**：原前端仅按镜像 ID 是否含 `windows` 字样判断，而自定义镜像 ID 形如 `custom-kvm-42e957647c`，导致被误判为 Linux（网络步骤显示 SSH 22 而非 RDP 3389）。现统一以镜像 `distro` 字段为准（新增共享工具 `utils/templateKind.ts`），后端 `IsWindows()` 同步改为 distro 判定，不再依赖 provisioner 或 ID 关键字。
  - **修复 Windows 虚拟机无法进入安装程序（致命）**：新建 Windows 虚拟机使用空磁盘，但 Domain XML 默认仅写 `<boot dev='hd'/>`，SeaBIOS 在空盘上直接以 `No bootable device` 中止，永不回退到安装光盘。现改为 `hd → cdrom` 回退链（`network` 引导时为 `network → cdrom → hd`）：空盘自动回退光盘启动安装，系统装好后优先硬盘引导、自动忽略仍挂载的安装 ISO。
  - **修复自定义 ISO 挂载**：`BootMedia` 增加物理存在性校验，杜绝写入 `__invalid_image_id__` 占位路径导致 `Cannot access storage file` 启动失败；运行中实例修改挂载时自动调用 `virsh change-media` 热插拔光驱。

### 15. 💿 额外挂载光盘、镜像选择器与任务假死修复 (Extra ISO / Media Picker / Task Stall Fix - v1.20.8)
- **💿 额外挂载光盘（独立光驱，不再抢占启动盘）**：原「自定义挂载 ISO 路径」的语义是**替换**镜像自带光盘，导致 PE 镜像（如 `firepe`）设了自定义 ISO 后就无法从自带的 PE 盘启动。现新增独立的 `extra_iso` 字段，挂载在**单独的 SATA 光驱（`sdc`）**上：
  - 镜像自带光盘（`hdb`）永远由镜像决定，额外光盘不会碰它 —— 可以「先用自带 PE 盘启动进入 PE，再从额外光驱读 Windows 安装盘」；
  - 支持**运行中热插拔**：域内尚无该光驱时先 `virsh attach-device` 挂上，之后换盘走 `change-media --insert/--update`、清空走 `--eject`，全程不影响启动盘；旧实例（域内无 SATA 控制器）无法热附加时会在下次开机自动生效；
  - 两个 Domain XML 生成器显式声明 `<controller type='sata' index='0'/>`，保证该光驱可被热插拔。
- **🖱️ 服务器镜像选择器**：新增 `GET /api/kvm-media`，汇总服务器上「已下载的注册镜像」与「镜像缓存目录内其它 `.iso`」（含名称、类型与体积）。前端「引导与硬件」弹窗把原来手填路径的输入框换成两个下拉选择器——「启动光盘覆盖」与「额外挂载光盘」，均提供「自定义路径…」回退；路径范围仍受原有白名单约束，不会暴露宿主机任意文件。
- **⏱️ 修复「一直卡在关机中 / 无法删除」任务假死（重要）**：任务队列按实例串行，两处长等待会把目标锁占满，使后续关机/删除只能排队、前端看起来像卡死：
  - **关机固定死等 45 秒**：系统盘还是空的 Windows 实例（正在跑安装程序或从未装系统）不响应 ACPI，必然耗满 45 秒才强断电。现用 `virsh domblkinfo` 读取系统盘真实分配量作为判据 —— 无数据可丢时优雅窗口 **45s → 5s**，盘上已有数据（含安装程序写入）仍保留完整 45 秒，不丢数据。
  - **开机最长死等 180 秒拿不到 IP**：运维在界面手改过系统标签的实例无法被 `FindImage` 解析，被误判为 Linux 而空等 IP。现兼容手写标签，不再阻塞。
  - 实测：新建 Windows 实例关机 **48 秒 → 6 秒**；全链路「新建 → 关机(6s) → 删除(2s)」通过。
- **📄 容器列表分页与批量勾选**：每页默认 **20** 条，可选项扩展为 **10 / 20 / 50 / 100** 并在本地记忆选择；表头复选框跨分页全选当前筛选结果，勾选后提供「取消选择」一键清空。
- **🧩 修复第三方 WinPE / WePE 镜像无法添加**：后端注册接口的 `switch req.Provisioner` 漏了 `windows-pe` 分支，导致选择「WinPE / WePE」模板必然报 `unsupported unattended installation template`。已补齐该分支（校验 amd64、归一 `distro`/`release`，WinPE 属纯引导镜像不生成无人值守应答文件）。
- **🌐 英文界面完整汉化**：补齐约 260 条中英对照词条并新增 10 条正则规则处理数字插值模板（`第 N 台容器` → `Container #N` 等），修掉 `Disk总线`、`Network InterfacesDriver`、`Page 台容器` 这类半中半英混合串；同时从源头简化了两句会产生混合翻译的文案。新增 4 个自检脚本（`frontend/scripts/i18n-{audit,check,coverage,duplicates}.mjs`）用于持续校验覆盖率与词典重复键。

### 16. 📝 快照备注：回退时一眼看清是哪一版 (Snapshot Note - v1.20.9)
- **需求背景**：快照列表原先只有时间、类型、创建者与体积，实例攒了多份快照后无法分辨「哪一份是升级前的基线、哪一份是装完环境后的」；回退时只能靠时间猜，风险很高。
- **改动内容**：
  - **数据结构**：`config.Snapshot` 新增 `description` 字段，SQLite `snapshots` 表新增同名列（`CREATE TABLE` 与 `ensureColumn` 迁移双写，旧库启动时自动补列并回填空串），读写与远程同步链路一并打通。
  - **详情页**：点「新建快照」后先弹出小窗口填写备注（200 字上限并实时计数，留空则只记时间与创建者），窗口内同时提供存储磁盘选择；若实例正在运行会提示「需先关机、完成后自动重启」。确认后才真正开拍。
  - **批量快照**：批量弹窗新增备注输入，一份备注写入本批全部快照（任务队列路径同样支持）。
  - **列表展示**：容器详情快照表与「快照管理」全局列表均新增独立「备注」列，长备注自动换行并以 `title` 悬浮显示全文；历史快照无备注时显示占位符 `-`。
  - **i18n**：新增词条已补英文对照，词典重复键保持 0。
- **涉及文件**：`backend/internal/config/{config,store_sqlite}.go`、`backend/internal/kvm/kvm.go`、`backend/internal/lxc/snapshot.go`、`backend/internal/api/{runtime,snapshots,taskqueue}.go`、`frontend/src/pages/{ContainerDetail,Snapshots}.tsx`、`frontend/src/components/BatchSnapshotModal.tsx`、`frontend/src/services/api.ts`、`frontend/src/utils/i18n.ts`。
- **实机验证**：
  - 走 HTTP 接口对测试实例 `ccc-good` 带备注拍快照，返回体与列表接口（单实例 + 全局）均正确回显 `description`；
  - 重启 `clicd` 服务后备注依然存在，确认 SQLite 列映射与迁移正确；
  - 走批量队列（`/batch-action` + `description`）拍快照，备注同样落到快照记录上；
  - 验证用的两份测试快照已删除，环境恢复原状（快照总数 14、实例 `ccc-good` 运行中且内网 IP 不变）。

### 17. 📊 快照占用拆分为「逻辑占用 / 实际独占」(Snapshot Usage Accuracy - v1.20.9)
- **问题背景**：面板只显示 `du` 口径的快照体积，容易被误读成"删掉就能回收这么多"。实测发现宿主文件系统（XFS `reflink=1`）会把快照做成写时复制克隆——`jsq-windows` 那份显示 **29.3 GiB** 的快照，用 `filefrag` 逐 extent 统计后真正的独占部分只有 **约 0.1~0.2 GiB**，其余 29.17 GiB 与运行中的磁盘共享同一批物理块。
- **实现（`backend/internal/api/snapshot_usage_linux.go`）**：`x/sys/unix` 没有 FIEMAP 封装，故直接实现 `FS_IOC_FIEMAP` ioctl，按 `FIEMAP_EXTENT_SHARED` 位统计单个快照镜像中"无其他文件引用"的字节数：
  - 语义：**实际独占 = 逻辑占用 − 共享字节 = 删除该快照能真正释放的空间**；
  - 配 20 秒 TTL 内存缓存（guest 持续写入会不断把共享块转为独占，结果本身在变），冷启动全量扫描约 1 秒，命中缓存后约 50ms；
  - 非 Linux 走 `snapshot_usage_fallback.go` 返回 `nil`；LXC 快照是 rootfs 目录树而非单个可克隆镜像，同样返回 `nil`，前端显示 `-`。
- **接口**：`config.Snapshot` 新增只读字段 `UniqueBytes`（`json:"unique_bytes,omitempty"`，**不落库**），全局列表 / 单实例列表 / 创建 / 同步四个返回快照的接口统一经 decorator 填充。
- **前端**：容器详情快照表与「快照管理」全局列表把原来的「大小 / 占用空间」拆成 **「逻辑占用」** 与 **「实际独占」** 两列，独占值绿色标注、不可测时显示 `-`，两列表头均带悬浮说明解释 reflink 共享以及"随系统继续写入逐步变大"的机制；全局表改为可横向滚动并加宽以容纳新列。
- **附带结论（写给后续维护者）**：`du` 会把 reflink 共享块在每个文件里各算一遍，因此面板上所有基于 `du` 的体积（含快照目录合计）都是**上界**而非真实物理占用；同一台机上实测 3 份 `jsq-windows` 快照逻辑合计 61.55 GiB，真正独占仅 18.14 GiB。**该特性依赖宿主文件系统**：ext4 或无 reflink 的 XFS 上同一份代码会退化为真实全量复制（几十 GB、耗时数分钟）。
- **实机验证**：接口返回 `unique_bytes` 数值与 `filefrag` 独立统计一致（`jsq-windows` 最新快照逻辑 29.26 GiB / 独占 0.18→0.24 GiB，随时间增长；`kylin-v10` 0.21 / 0.00 GiB），全局列表首次 1.02s、缓存后 0.05s，LXC 快照正确返回 `null`；前端 `tsc + vite build` 通过，i18n 重复键 0。

### 18. 🛠️ 三个由审计日志暴露的稳定性缺陷修复 (Restore Permissions / Image Delete Guard / VirtIO ISO - v1.20.9)
- **① 快照/备份还原后虚拟机无法开机（Permission denied）**
  - **现象**：审计日志连续报 `virsh start failed: Cannot access storage file '.../vm-25/disk.qcow2' (as uid:64055): Permission denied`。
  - **根因**：快照与备份目录以 `0700 root:root` 创建，`RestoreSnapshot` / `RestoreBackup` 用 `copyTree` 把它们原样复制回 `instances/vm-X`，非 root 的 QEMU 进程（libvirt-qemu，uid 64055）连目录都进不去。
  - **修复**：新增 `kvmQEMUIdentity()`（探测 `libvirt-qemu`/`qemu` 的用户与主组）与 `fixKVMInstancePermissions()`，在 **每次开机前** 以及两个还原流程 `copyTree` 之后调用。目录保持 `0700`（避免把租户磁盘暴露给其它本地账号），只把属主交给 QEMU 用户；探测不到 QEMU 用户时退回 `0755/0644` 保证仍能启动。同类修复曾在 `215a65f` 提交过，但在 main 分支历史重置中丢失，本次连同回归测试一起重新落地。
- **② 删除镜像导致实例永久损坏（Cannot access backing file）**
  - **现象**：`kylin-v10`(vm-3) 反复报 `Cannot access backing file '.../custom-kvm-9a78b2756f.qcow2' ...: No such file or directory`，全盘已无该文件副本，实例数据不可恢复。
  - **根因**：删除镜像只校验 `container.Template == imageID`，而 `Template` 是运维可随意改写的展示标签（`kylin-v10` 的标签是 `kylin`，`jsq-win-2019` 是 `windows`）。用户移除 `custom-kvm-9a78b2756f` 后，vm-3 的 overlay 立刻失去 backing file。
  - **修复**：新增 `kvm.InstancesUsingImage()`，用 `qemu-img info -U --backing-chain --output=json` 读取**真实 qcow2 依赖链**（`-U` 才能读运行中实例），并按文件名主干匹配（`ImagePath` 对 windows 镜像会猜成 `.iso`，按路径比对必然失配）；母盘缺失导致 `qemu-img` 打不开时，回退**直接解析 qcow2 头**读 backing 路径，避免漏判。`handleCustomKVMImageDelete` 与 `HandleImageDelete` 两处删除前拦截并指名占用的实例。
- **③ VirtIO 驱动光盘实为 4KB 网页（Guest Agent 装不上的真正原因）**
  - **根因**：镜像源 `fedorapeople.org` 对非浏览器请求返回 Anubis 反机器人 HTML 页面且状态码 **200**，而 `ensureVirtioWinISO` 只判断 `os.Stat` 存在即复用，于是这份 4473 字节的 HTML 被长期当成 `virtio-win.iso` 挂给虚拟机——客户端看到的是一张无效光盘，驱动与 `qemu-ga` 自然装不上。
  - **修复**：新增 `virtioWinISOUsable()`（体积 ≥100MB、拒绝 HTML 前缀、校验 ISO9660 在 `0x8001` 处的 `CD001` 签名），缓存文件校验失败即删除重下；下载走 `downloadFileWithValidator` 并校验临时文件后才原子改名。
  - **源可配置**：若上游镜像不可达，可用 `CLICD_VIRTIO_WIN_ISO_URL` 覆盖（逗号/空格分隔多个地址，支持 `http(s)://` 或**本机文件路径**），systemd 单元已通过 `/etc/clicd/network.env` 透传环境变量；也可直接把真 ISO 放到 `/var/lib/clicd/images/kvm/virtio-win.iso`。
- **涉及文件**：`backend/internal/kvm/{kvm,kvm_test}.go`、`backend/internal/api/images.go`。
- **实机验证**：
  - 手动按修复逻辑归一化 vm-25 权限后 `virsh start vm-25` 成功、`domstate` 为 running；
  - 故意把 vm-3 目录改回 `root:root 0700`，经面板触发开机后权限被自动改回 `libvirt-qemu:kvm`（验证开机自愈接线）；
  - 用被测镜像的真实依赖链调用生产函数：`custom-kvm-f58ab36672 -> [jsq-windows]`、`custom-kvm-9a78b2756f -> [kylin-v10]`（后者靠 qcow2 头兜底识别）；
  - 对上述两个镜像分别调用「删除缓存」与「移除第三方镜像源」接口，均返回 409 并指名 `jsq-windows`，母盘与镜像记录完好（测试前用 reflink 克隆做了安全网，测后删除）；
  - VirtIO：把 4KB 的坏文件交给挂载接口，日志出现 `cached virtio-win.iso is unusable (... only 4473 bytes)`，随后因上游返回 HTML 被拒绝并给出可操作报错——确认"假 ISO 静默挂载"的通道已封死；
  - `go vet` 与 `go test ./...` 全绿（新增 `TestFixKVMInstancePermissionsMakesRestoredInstanceReachable`）。
- **遗留事项**：`kylin-v10`(vm-3) 的母盘 `custom-kvm-9a78b2756f.qcow2` 已不可恢复，需重新下载该镜像（记录仍指向 `cloud.debian.org` bookworm `latest`，若上游镜像已更新则与旧 overlay 不一致，建议直接重装该实例）或删除该实例。





## Features / 功能介绍

### English

| Area | What CLICD provides |
| --- | --- |
| Virtualization | Manage LXC containers and KVM virtual machines from one panel, including create, reinstall, start, stop, restart, delete, password reset, expiry control, and batch actions. |
| Images and templates | Built-in template and image management for Ubuntu, Debian, Alpine, CentOS, Arch Linux, Fedora, Rocky Linux, and other common distributions. Images can be enabled, disabled, downloaded, cancelled, or removed from cache. |
| Networking | NAT4 port quotas, random available port allocation, TCP/UDP port mappings, public IPv4 pool management, IPv6 prefix detection, IPv6 status checks, and per-container IPv6 assignment. |
| Resource control | CPU, memory, disk, swap, bandwidth usage, traffic reset, traffic limit, and resource limit management, with automatic shutdown behavior for expired or over-quota containers. |
| Console access | Browser-based WebSSH and WebVNC ticket access, so users can open terminals or consoles without manually exchanging credentials. |
| Snapshots | Snapshot overview, per-container snapshots, create/delete/restore operations, scheduled snapshots, and quota controls. |
| Security | Conntrack-based security alerts for port scans, lateral scans, brute-force behavior, SMTP abuse, UDP reflection, mining ports, proxy/VPN/Tor usage, plus security logs, summaries, and configurable settings. |
| Accounts and audit | Delegated sub-user links, sub-user password rotation, per-user container permissions, audit logs, login logs, and API key management. |
| Automation | Versioned REST API under `/api/v1`, task queue endpoints, batch create/action endpoints, and a Mofang finance integration module packaged automatically by GitHub Actions. |
| Operations | Dashboard statistics, host resource overview, routing overview, swap management, CLI-only mode, and release artifacts generated by GitHub Actions. |

### 中文

| 模块 | CLICD 提供的能力 |
| --- | --- |
| 虚拟化管理 | 在同一个面板里管理 LXC 容器和 KVM 虚拟机，支持创建、重装、开机、关机、重启、删除、重置密码、到期时间和批量操作。 |
| 镜像与模板 | 内置模板和镜像管理，支持 Ubuntu、Debian、Alpine、CentOS、Arch Linux、Fedora、Rocky Linux 等常见发行版，镜像可按需下载、取消、启用、禁用和清理缓存。 |
| 网络能力 | 支持 NAT4 端口配额、随机可用端口、TCP/UDP 端口映射、公网 IPv4 池管理、IPv6 前缀检测、IPv6 状态检查和容器级 IPv6 分配。 |
| 资源限制 | 支持 CPU、内存、磁盘、Swap、独立上行/下行带宽、读/写 I/O 限速、流量重置、流量限制和资源限制管理；容器到期或超额后可自动关机，避免资源和流量失控。 |
| 远程控制 | 内置 WebSSH 和 WebVNC 票据访问，用户可以直接在浏览器打开终端或控制台，不需要手动复制连接信息。 |
| 快照能力 | 支持快照总览、容器快照、创建快照、删除快照、恢复快照、计划快照和快照配额。 |
| 安全告警 | 基于 conntrack 做轻量安全检测，可识别端口扫描、横向扫描、爆破倾向、SMTP 滥用、UDP 反射、挖矿端口、代理/VPN/Tor 等风险，并提供安全日志、汇总和设置项。 |
| 账号与审计 | 支持子用户管理链接、子用户密码轮换、按容器授权、操作日志、登录日志和 API Key 管理，适合分发给下游用户或拼车用户。 |
| 自动化接入 | 全量接口统一使用 `/api/v1`，覆盖任务队列、容器、镜像、网络、流量、安全、批量创建和批量操作；同时提供魔方财务对接模块，并由 GitHub Actions 自动打包发布。 |
| 运维入口 | 提供总览统计、主机资源、路由概览、Swap 管理、CLI-only 模式和 GitHub Actions 自动发布产物，便于在小型节点上长期维护。 |

## Technology Stack / 技术栈

- Backend: Go, net/http, LXC, KVM/libvirt, cgroup v2, iptables, conntrack
- Frontend: React, TypeScript, Vite, Tailwind CSS, lucide-react, xterm.js
- Runtime: Linux, systemd, LXC, KVM/QEMU
- Build: GitHub Actions, Node.js 20, Go 1.24

## Preview / 预览
![alt text](/img/image-2.png)
![alt text](/img/image-3.png)
![alt text](/img/image-4.png)
![alt text](/img/image-5.png)


## Disclaimer/免责声明

This open-source software does not distribute Windows system images, nor does it provide any means to bypass or circumvent Windows activation mechanisms.

All download links provided within the software point to resources officially supplied by Microsoft. Users of this software are responsible for obtaining the appropriate licenses from Microsoft before using any Windows operating system downloaded through these links. This project does not bypass activation requirements for installed systems, nor does it assume any responsibility for the consequences of users' actions when using this software.

This open-source software is intended solely for educational purposes, specifically for learning the principles of LXC and KVM. The copyright for the Windows logo and related icons belongs to Microsoft/Windows.

本开源软件不提供任何 Windows 操作系统镜像的分发服务，也不包含任何绕过、破解或免除 Windows 激活机制的功能。

软件内涉及的 Windows 系统下载链接均由微软官方提供。使用者在下载、安装和使用相关 Windows 系统时，应自行向微软或其授权渠道购买并获得相应的软件许可。本项目不会对安装后的 Windows 系统进行任何形式的激活绕过、破解或免激活处理。

对于使用者因使用本软件而产生的任何行为及其后果，包括但不限于软件许可、系统使用、数据丢失、法律责任或其他相关问题，本项目及其开发者不承担任何责任。

本开源软件仅供学习和研究 LXC、KVM 等虚拟化技术原理之目的使用，不得用于任何违反适用法律法规、软件许可协议或第三方权益的行为。

本软件中涉及的 Windows 名称、标识、图标及相关知识产权均归 Microsoft Corporation 及其权利人所有。本项目与微软公司不存在任何关联、授权或合作关系。
## Thanks / 鸣谢
- [Nodeseek.com](https://www.nodeseek.com) — 一个专注于服务器的社区
- [Linux.do](https://linux.do) — 一个充满灵感的科技社区


## Star History

[![MengMengCode/CLICD Star History](http://mengmeng.meteor-history.com/api/embed/MengMengCode/CLICD.svg?sig=YT8i1bxihL6_GcFAa0CWRbQb35-B0XXyh-ZAxIsmV0U&theme=light&style=xkcd&color=dd4528&background=ffffff&textColor=000000&width=900&height=600&lineWidth=3&showTitle=true&showLegend=true&showDots=false&v=3)](https://meteor-history.com)
