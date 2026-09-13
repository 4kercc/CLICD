# CLICD 项目架构、功能设计与演进记录全景文档 (Project Documentation)

本文档记录了 **CLICD (LXC/KVM 虚拟化管理面板)** 的系统全景架构、模块代码分工、关键技术设计、历史演进记录（涵盖 v1.20 ~ v1.20.3 核心特性）以及常用运维与部署指令。

---

## 📌 项目基本信息
- **项目名称**：CLICD (Container & KVM Lifecycle Controller Daemon)
- **当前版本**：`v1.20.3`
- **代码仓库**：[https://github.com/4kercc/CLICD](https://github.com/4kercc/CLICD)
- **后端技术栈**：Go 1.24+ (原生标准库 + SkyLight / Libvirt / LXC / Conntrack 深度调用，无重型第三方框架)
- **前端技术栈**：React 18 + TypeScript + Vite + Tailwind CSS + Lucide Icons
- **支持虚拟化引擎**：
  - **LXC**：轻量级容器（支持 cgroup v2 资源隔离、LXC 模板、网卡限速、磁盘限额、rootfs 用户命名空间 UID/GID 映射）
  - **KVM / QEMU**：全虚拟化（支持 Windows 10/11/Server、Linux 全发行版、Windows PE / FirPE / WePE 救援系统、virtio-win 驱动挂载、QEMU Guest Agent 协同）

---

## 🏗️ 模块划分与代码架构

### 1. 后端目录结构 (`backend/`)
- `main.go`：服务入口，解析 `server` 与 `cli` 运行参数，初始化网络与端口映射，优雅捕获系统退出信号。
- `internal/server/`：
  - `server.go`：HTTP/HTTPS 路由分发器，静态 Web 资产 Embed 嵌入与 SSL Let's Encrypt 证书热加载。
- `internal/api/`：
  - `handlers.go`：核心容器/虚拟机列表、创建、删除、开机、关机、重启、密码重置等接口。
  - `runtime.go`：多虚拟化引擎（LXC/KVM）抽象适配层，路由具体操作至底层 Manager。
  - `security.go`：轻量安全引擎（Conntrack 流量监测、端口扫描、横向移动、多周期挖矿判定模型）。
  - `snapshots.go`：快照与全量备份接口，支持单项/批量触发远程异地存储同步。
  - `storage.go`：本地挂载磁盘检测、存储池 CRUD、存储池连通性测试（`/storage/test`）与一键全量备份同步（`/storage/sync-all`）。
  - `totp.go`：两步验证（2FA/TOTP RFC 6238）密钥生成、二维码生成与登录动态验证。
- `internal/kvm/`：
  - `kvm.go`：Libvirt XML 模板生成器（支持 Boot Order、网卡驱动模型、磁盘总线、QEMU GA 交互、fsinfo 真实磁盘读取、在线热扩容）。
- `internal/lxc/`：
  - `lxc.go`：LXC 容器生命周期管理、cgroup v2 资源限制（vCPU/内存/磁盘限速/带宽流控）、网络挂载与 SSH 自动注入。
- `internal/storage/remote/`：
  - `remote.go`：远程存储驱动接口（支持 **SFTP**、**WebDAV**、**MinIO / AWS S3** 原生驱动与连接测试）。
  - `sync.go`：快照与全量备份异步双写引擎、多源拉取还原（Local / Remote）与批量同步逻辑。
- `internal/config/`：
  - `config.go`：全局配置文件（`/var/lib/clicd/config.json`）序列化与并发安全锁。
- `internal/version/`：
  - `version.go`：定义全局版本号（`1.20.1`）。

### 2. 前端目录结构 (`frontend/`)
- `src/pages/`：
  - `Containers.tsx`：容器列表页面（支持软加载/懒加载状态呈现、多维度搜索筛选、批量控制）。
  - `ContainerDetail.tsx`：虚拟机/容器详情控制台（实时 CPU/内存/磁盘环形图、NAT 规则配置、Guest Agent 状态与 OS 引导安装弹窗、快照管理、全量备份、VNC/WebSSH 控制台）。
  - `Storage.tsx`：存储管理中心（本地磁盘挂载状态、远程存储池 SFTP/WebDAV/MinIO 管理、一键全量同步按钮）。
  - `Snapshots.tsx`：快照与备份总览。
  - `Security.tsx`：安全告警与威胁日志。
- `src/services/api.ts`：Axios API 封装层，定义全部 TypeScript 类型与 REST 请求。

---

## 🚀 核心优化与演进记录 (v1.20 ~ v1.20.1)

### 一、 引导管理与灵活硬件驱动选型 (KVM Boot & Hardware Flexibility)
1. **第一启动项动态切换 (Boot Order)**：
   - 支持自由切换 **硬盘优先 (Hard Disk)**、**光盘/ISO 优先 (CD-ROM)** 或 **网络 PXE 引导**。
   - 彻底解决了 Windows/PE 重启后再次陷入光盘安装界面的顽疾。
2. **虚拟硬件模型适配**：
   - **网卡驱动 (NIC Model)**：支持切换为高性能 `VirtIO`（Linux原生、Windows需驱动）、`Intel e1000e`（免驱千兆兼容）或 `RTL8139`。
   - **磁盘总线 (Disk Bus)**：支持切换为 `VirtIO Block (vda)`、`SATA AHCI (sda)`、`IDE (hda)`。

### 二、 快照 (Snapshot) 与 备份 (Backup) 架构分离 + 零停机在线热快照
1. **零停机在线热快照 (Live Snapshot)**：结合 QEMU Guest Agent `fsfreeze` 冻结与 COW 增量捕获，**打快照无需关机**，1 秒内完成。
2. **自动分层 COW 轻量化**：自动将单体大镜像转换为 `Base (只读基盘) + Overlay (轻量增量层)` 架构，单次快照体积从数十 GB 暴降至 **几十 KB ~ 几十 MB**。
3. **独立的 Full Backup 全量备份体系**：新增独立接口与数据表，备份时执行全量打平与 `qcow2` 压缩打包归档。

### 三、 在线与离线磁盘动态扩容 (Live Disk Resize)
- 支持在面板一键调整 KVM 磁盘容量。
- **在线扩容**：虚拟机开机状态下调用 `virsh blockresize`，并由 Guest Agent 自动触发内部系统文件系统扩展（Windows C 盘 extend，Linux growpart / resize2fs）。
- **离线扩容**：关机状态下调用 `qemu-img resize` 安全扩容。

### 四、 外部磁盘镜像导入与 PVE/KVM 迁移向导
- 支持直接输入宿主机外部镜像路径（如 PVE 导出的 `vm-301-disk-0.qcow2` 或 raw/vmdk 镜像）。
- 支持一键转为 Base 只读基盘并自动挂载增量层，实现从 PVE 到 CLICD 的极速平滑迁移。

### 五、 QEMU Guest Agent 与多系统协同安装
1. **操作系统感知适配**：
   - **Windows**：提供「一键挂载驱动光盘」与「弹出光盘」控制，配合 `virtio-win.iso` 安装 `vioserial` 及 QEMU-GA。
   - **Linux**：隐藏多余光盘挂载，提供多发行版（Debian / Ubuntu / CentOS / Rocky / Alpine）自适应一键安装命令。
2. **真实磁盘容量读取**：
   - 解决 KVM 物理层仅显示 2GB Overlay 磁盘的问题，通过 `guest-get-fsinfo` 实时解析 Windows `C:\` 分区真实使用率。

### 六、 挖矿智能深度识别引擎 (v1.20.1)
- **长连接生命周期追踪 (`miningTracker`)**：监测长连接在多个扫描周期（>45s）的活跃存续情况。
- **状态加权评分模型**：关联 TCP `ESTABLISHED` 状态、Stratum 握手协议特征与主流矿池威胁情报库，置信度 Score $\ge 70$ 才告警，彻底消除开发环境、Node.js 与通用代理的误报。

### 七、 多协议远程存储与异地容灾双写 (v1.20.1)
1. **原生多协议客户端**：
   - **SFTP**：基于 SSH 协议流式上传下载。
   - **WebDAV**：标准 RFC 4918 HTTP 协议支持。
   - **MinIO / AWS S3**：纯 Go 实现 AWS Signature Version 4 签名。
2. **自动化异步双写与集中同步**：
   - 存储池开启同步后，创建快照/备份时**后台自动异步双写上传**。
   - 在「存储管理」页面提供 **「一键同步所有快照与备份」** 与单个存储池同步按钮。
3. **多源灾备恢复**：恢复快照/备份时，支持自由选择 **「从本地极速还原」** 或 **「从远程存储拉取还原」**。

### 八、 实时传输进度跟踪与面板体验全面打磨 (v1.20.2)
1. **传输进度实时可视化**：
   - 远程同步快照/备份或从远程存储拉取恢复时，提供毫秒级进度轮询（`/storage/progress`），动态展示当前传输文件、实时传输速率（MB/s）、已传输量与完成百分比。
2. **Guest Agent 模块与安装引导优化**：
   - 状态栏显示绿色连接徽标与黄色待安装提示，弹窗集成 Windows（光盘挂载）与 Linux（一键脚本）双模式自主切换 Tab。
3. **控制台交互与安全防护升级**：
   - **双击重命名**：主机名和系统标签支持双击行内快速编辑，便于管理多台同配置虚拟机。
   - **红色高亮删除与二次防误触**：危险操作独立红色展示，并引入两次弹窗确认，彻底杜绝误删风险。

### 九、 虚拟化安全加固与稳定性防卡死重构 (v1.20.3)
1. **安全修复（严重）**：
   - 「导入外部磁盘」源路径强制白名单（存储池 / KVM 数据目录 / 镜像缓存，含符号链接解析），并收紧为仅管理员可用，彻底封堵子用户越权读取宿主机任意文件的漏洞。
2. **并发与锁重构（高）**：
   - KVM/LXC 快照、备份、还原的全局互斥锁收窄至「轮转删除」与「配置登记」瞬时阶段；长磁盘 I/O 由单机维度互斥锁（`acquireVMLock` / `acquireLXCLock` / `acquireSyncLock`）保护，多虚拟机操作互不阻塞。
3. **热快照防撕裂（高）**：
   - 有 Guest Agent：`fsfreeze` 冻结整个拷贝窗口（应用一致性）。
   - 无 Agent：`virsh snapshot-create-as --disk-only --atomic --no-metadata` 原子外部快照 + `blockcommit --active --pivot` 回合并清理，拷贝统一 `qemu-img convert -U`。
   - 服务启动时自动解冻遗留冻结虚拟机（`ThawAllRunningVMs`），优雅关机等待延长至 45 秒。
4. **超时与输入校验**：
   - `virsh` 热路径（domstate / domifaddr / domstats / guest-ping / guest-exec 等）统一 5–30 秒硬超时封装（`virshCombinedOutput` / `virshOutput`）。
   - LXC PID 拼接前强制数字校验；主机名 / 系统标签统一 64 字符与控制字符校验。

---

## 🧪 dev 分支演进记录（对照 PVE 的正常使用优化）

> 以下改动位于 `dev` 分支，按优先级对照 Proxmox VE 补齐正常使用体验。

### 一、 备份与快照全面免停机（对齐 PVE vzdump snapshot/suspend 模式）
1. **KVM 在线全量备份**：运行中的虚拟机不再需要关机备份。有 Guest Agent 时 `fsfreeze` 冻结整个拷贝窗口（应用一致性）；无 Agent 时走 `snapshot-create-as --disk-only --atomic` 原子外部快照 + `blockcommit --active --pivot` 合并回原盘，磁盘统一 `qemu-img convert -c -U` 打平压缩读取。关机虚拟机保持原冷拷贝路径。
2. **LXC 免停机快照**：运行中的容器改用 cgroup freezer（`lxc-freeze` → 拷贝 → `lxc-unfreeze`），进程仅暂停毫秒级，替代原先"关机→冷拷贝→开机"的整段停机窗口；宿主机命名空间下存在可见挂载或 freeze 失败时自动回退冷路径，解冻失败重试三次并高亮告警。

### 二、 定时全量备份与备份轮转（对齐 PVE Backup Jobs + Keep Last）
1. 每 VM 支持 `BackupSchedule`（间隔≥24h、执行时刻、保留份数），由每分钟调度器驱动；API 默认保留 3 份并按日期轮转删除最旧备份，防止磁盘被悄悄占满。
2. 新端点 `POST /api/.../containers/{id}/backups/schedule`；备份列表响应携带 schedule 字段；前端备份面板新增「定时备份」设置与状态横幅。
3. 每份全量备份落盘后计算并持久化 **SHA256 校验和**（`backups.checksum` 列）。

### 三、 远程双写与多源恢复可靠性
1. 快照/备份上传失败自动重试 3 次（线性退避，共享上下文限时）。
2. 远程拉取恢复逐文件校验：`disk.qcow2` 下载失败即整体失败并删除半成品文件；有校验和的备份做 SHA256 比对，杜绝还原损坏数据。
3. 还原时仅在本地缺盘文件时才从远程拉取，且拉取失败会向上传播错误（原先错误被静默忽略）。

### 四、 生命周期可靠性（对齐 PVE Start at boot / qm reboot）
1. **期望状态实时持久化**：KVM 15 秒网络同步循环与新增的 LXC 60 秒状态对账器（`lxc-ls` 单次探测）在发现配置与实际状态背离（如 guest 内自关机）时，同步修正 `RestoreOnHostBoot` 标志——宿主机断电/硬重启后恢复行为与真实期望一致。
2. **KVM 热重启**：运行中的 VM 优先 `virsh reboot`（客户机内 ACPI 重启，QEMU 进程不动，秒级完成），失败回退原关机+冷启动；重启后自动等待 IP 并重放端口映射与防火墙规则。

### 五、 网络与磁盘细节
1. **有状态防火墙**：default DROP 策略自动插入 `conntrack ESTABLISHED,RELATED` 放行（iptables/ip6tables，位置在 DROP 之上、用户规则之下），回包不再需要镜像出站规则，对齐 PVE 有状态防火墙语义。
2. **磁盘 discard=unmap**：KVM 系统盘 driver 启用 unmap，guest 内 fstrim/TRIM 可回收 qcow2/overlay 链占用，防止镜像只增不减。
3. **快照/备份恢复后权限自动规范化 (`fixKVMInstancePermissions`)**：快照目录以 0700 创建，恢复复制回实例目录后自动重置目录 0755 / 文件 0644 并递归 `chown libvirt-qemu:libvirt-qemu`，彻底解决恢复后开机报 `Cannot access storage file ... Permission denied (as uid:64055)` 的致命问题。

### 六、 WebVNC 性能与剪贴板/打字辅助体验升级
1. **本地光标渲染 (`showDotCursor`)**：鼠标在浏览器内移动时本地绘制微点光标，消除等待服务器回显带来的拖动粘滞感。
2. **双态压缩模式在线切换**：默认「流畅模式」（质量 2 + 压缩 8，公网下帧体积缩减 60-80% 提升帧率），支持一键切换「清晰模式」（质量 7 + 压缩 4 保证精细读写）。
3. **内置剪贴板与模拟打字工具箱 (Type Text)**：
   - 顶部工具栏增加「📋 剪贴板」浮动抽屉。
   - **模拟键盘键入 (Type Text)**：将文本按字符映射为 X11 Keysym，以 12ms 间隔自动敲入虚拟机当前光标处，**100% 免驱通用**，支持 Windows 锁屏密码与 Linux 纯终端长命令输入。
   - **双向剪贴板同步**：支持一键将本地文本发送至 VNC 剪贴板，并实时捕获虚拟机内的 `Ctrl+C` 复制内容。

### 七、 动态内存气球守护机制 (`BalloonGuard`，对齐 ESXi / PVE 调度)
1. **三态智能自适应**：
   - 常态模式（母鸡物理内存 <85%）：气球完全休眠，禁止抢夺内存，虚拟机独享全部物理内存，消除 Windows 内存颠簸与 CPU 飙高。
   - 压力模式（母鸡物理内存 ≥85%）：后台守护协程自动从空闲率高的 KVM 虚拟机收回安全闲置内存（保留 512MB 安全缓冲），防止宿主机 OOM。
   - 恢复模式（母鸡物理内存 ≤75%）：压力解除后自动恢复所有虚拟机至全额原始配置。
2. **全套 Hyper-V Enlightenments 加速**：补齐 `tlbflush`、`ipi`、`stimer`、`synic`、`vpindex`、`runtime` 等 9 大半虚拟化加速特性，消灭无谓的 VM-Exits。

---

## 🛠️ 运维与部署常用指令

### 1. 一键安装与更新
```bash
# 一键安装 / 更新至最新稳定版
curl -fsSL https://raw.githubusercontent.com/4kercc/CLICD/main/install.sh | sudo sh

# 卸载 CLICD
curl -fsSL https://raw.githubusercontent.com/4kercc/CLICD/main/install.sh | sudo sh -s -- uninstall
```

### 2. 服务管理与排查
```bash
# 查看服务状态
systemctl status clicd --no-pager

# 重启服务
systemctl restart clicd

# 查看实时日志
journalctl -u clicd -f -n 50
```

### 3. 本地编译与打包流程
```bash
# 1. 前端构建
cd frontend && npm install && npm run build

# 2. 同步 Web 静态资源到后端
cp -r frontend/dist/* backend/internal/server/web/

# 3. 交叉编译 Linux 二进制
cd backend
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X clicd/internal/version.Version=1.20.3" -o ../build/clicd-linux-amd64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w -X clicd/internal/version.Version=1.20.3" -o ../build/clicd-linux-arm64 .
```
