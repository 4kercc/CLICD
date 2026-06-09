<p align="center">
  <img src="frontend/public/favicon.svg" width="96" alt="CLICD">
</p>

<h1 align="center">CLICD</h1>

<p align="center">
  <img alt="Release" src="https://img.shields.io/github/v/release/MengMengCode/CLICD?style=flat-square">
  <img alt="Stars" src="https://img.shields.io/github/stars/MengMengCode/CLICD?style=flat-square">
  <img alt="Forks" src="https://img.shields.io/github/forks/MengMengCode/CLICD?style=flat-square">
  <img alt="Downloads" src="https://img.shields.io/github/downloads/MengMengCode/CLICD/total?style=flat-square">
  <img alt="Last Commit" src="https://img.shields.io/github/last-commit/MengMengCode/CLICD?style=flat-square">
  <img alt="License" src="https://img.shields.io/github/license/MengMengCode/CLICD.svg?style=flat-square">
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22-00ADD8?style=flat-square&logo=go&logoColor=white">
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

CLICD is a lightweight virtualization management panel for LXC and KVM, featuring a web console, CLI management, batch operations, image management, NAT networking, IPv6 allocation, WebSSH, VNC access, resource controls, bandwidth limiting, and security alerting.
It is designed for managing LXC containers and KVM virtual machines on VPS servers, and is particularly suitable for environments that require bulk provisioning and delegated access management through sub-user management links.

CLICD 是一个面向 LXC/KVM 的轻量虚拟化管理面板，提供 Web 控制台、CLI、批量任务、镜像管理、NAT 端口、IPv6 分配、WebSSH、VNC、资源限制、流量限制和安全告警能力。它适合用来管理小型 VPS 上的 LXC 容器和 KVM 虚拟机，也适合需要批量创建和分发子用户管理链接的场景。

## Features / 功能介绍

### English
1. Supports Ubuntu, Debian, Alpine, CentOS, Arch Linux, Fedora, Rocky Linux, and other operating system images. Images can be downloaded on demand through the image management interface. For hosts with limited resources, lightweight distributions such as Alpine are recommended.
2. Supports WebSSH management, allowing users to access container terminals directly from the browser without manually copying SSH credentials.
3. Supports NAT4 port quotas, port forwarding, and protocol restrictions, as well as public IPv6 allocation. IPv6 assignment requires the host machine to have a routable IPv6 prefix.
4. Supports both inbound and outbound traffic limits. Containers are automatically powered off when configured limits are reached, preventing bandwidth overuse.
5. Supports container expiration dates. Expired containers are automatically shut down, and delegated users lose access until an administrator extends the expiration period.
6. Includes lightweight conntrack-based security monitoring. The system does not store full logs of normal connections, but generates audit alerts for suspicious activities such as port scanning, lateral scanning, brute-force attempts, SMTP abuse, UDP reflection attacks, cryptocurrency mining ports, and proxy/VPN/Tor usage.
7. Supports delegated management links. Administrators can assign specific containers to sub-users, while ensuring that each user can only manage the containers explicitly authorized to them.
8. Provides a REST API for automating the management of containers, tasks, images, networking, traffic controls, and security alerts.
9. Supports operating entirely through the CLI. When the web console is not required, administrators can stop and disable the systemd service and launch CLI-only mode using `clicd cli --no-web`.

### 中文
1. 支持 Ubuntu、Debian、Alpine、CentOS、Arch Linux、Fedora、Rocky Linux 等系统镜像。镜像可以在镜像管理中按需下载；如果宿主机资源比较小，建议优先选择 Alpine 这类轻量镜像。
2. 支持 WebSSH 管理，可以在浏览器里一键进入容器终端，不需要手动复制 SSH 密码。
3. 支持设置 NAT4 端口数量、NAT 端口映射和协议限制，并支持分配公网 IPv6。IPv6 分配要求宿主机本身拥有可路由的 IPv6 地址段。
4. 支持单向和双向网络流量限制。达到限制后容器会自动关机，避免流量超额。
5. 支持设置容器有效期。到期后容器会自动关机，子用户无法继续操作，只有管理员重新设置延期日期后才能恢复使用。
6. 内置基于 conntrack 的轻量安全告警。系统不会保存完整正常连接日志，但会对端口扫描、横向扫描、爆破倾向、SMTP 滥用、UDP 反射、挖矿端口、代理/VPN/Tor 等可疑行为生成告警并写入审计日志。
7. 支持子用户管理链接，管理员可以把指定容器分发给拼车用户，子用户只能管理自己被授权的容器。
8. 支持 API 接入，可以通过 API 完成容器、任务、镜像、端口、流量、安全告警等功能的自动化控制。
9. 支持仅使用 CLI 管理。需要关闭 Web 控制台时，可以停止并禁用 systemd 服务，然后使用 `clicd cli --no-web` 进入命令行模式。

## Technology Stack / 技术栈

- Backend: Go, net/http, LXC, KVM/libvirt, cgroup v2, iptables, conntrack
- Frontend: React, TypeScript, Vite, Tailwind CSS, lucide-react, xterm.js
- Runtime: Linux, systemd, LXC, KVM/QEMU
- Build: GitHub Actions, Node.js 20, Go 1.22

## Installation / 安装

One-click Install / 一键安装：

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/CLICD/main/install.sh | sudo sh
```

One-click Uninstall / 一键卸载：

```bash
curl -fsSL https://raw.githubusercontent.com/MengMengCode/CLICD/main/install.sh | sudo sh -s -- uninstall
```

![alt text](/img/image.png)
![alt text](/img/image-1.png)
![alt text](/img/image-2.png)


## Disclaimer/免责声明

This open-source software does not distribute Windows system images, nor does it provide any means to bypass or circumvent Windows activation mechanisms.

All download links provided within the software point to resources officially supplied by Microsoft. Users of this software are responsible for obtaining the appropriate licenses from Microsoft before using any Windows operating system downloaded through these links. This project does not bypass activation requirements for installed systems, nor does it assume any responsibility for the consequences of users' actions when using this software.

This open-source software is intended solely for educational purposes, specifically for learning the principles of LXC and KVM. The copyright for the Windows logo and related icons belongs to Microsoft/Windows.

本开源软件不提供任何 Windows 操作系统镜像的分发服务，也不包含任何绕过、破解或免除 Windows 激活机制的功能。

软件内涉及的 Windows 系统下载链接均由微软官方提供。使用者在下载、安装和使用相关 Windows 系统时，应自行向微软或其授权渠道购买并获得相应的软件许可。本项目不会对安装后的 Windows 系统进行任何形式的激活绕过、破解或免激活处理。

对于使用者因使用本软件而产生的任何行为及其后果，包括但不限于软件许可、系统使用、数据丢失、法律责任或其他相关问题，本项目及其开发者不承担任何责任。

本开源软件仅供学习和研究 LXC、KVM 等虚拟化技术原理之目的使用，不得用于任何违反适用法律法规、软件许可协议或第三方权益的行为。

本软件中涉及的 Windows 名称、标识、图标及相关知识产权均归 Microsoft Corporation 及其权利人所有。本项目与微软公司不存在任何关联、授权或合作关系。
## Thanks/鸣谢

- [Linux.do](https://linux.do) — 一个充满灵感的科技社区

## Star History

<a href="https://www.star-history.com/?repos=MengMengCode%2FCLICD&type=date&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=MengMengCode/CLICD&type=date&theme=dark&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=MengMengCode/CLICD&type=date&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=MengMengCode/CLICD&type=date&legend=top-left" />
 </picture>
</a>
