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
