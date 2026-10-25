# CLICD MCP Server

把 CLICD 面板的 REST API 封装成 MCP（Model Context Protocol）工具，让 AI 助手可以直接查询模板、检查宿主机容量、下载镜像、创建/开关机/重装 LXC 容器与 KVM 虚拟机、管理快照和任务队列。

支持**多台面板**：每个工具都带一个可选参数 `panel`，用于指定操作哪一台。

## 要求

- Node.js 18+（本机已验证 v24）
- 每台面板一个 CLICD API Key（`clicd_sk_...`）
- 面板地址可访问（默认 `http://<面板地址>:8999`）

## 安装

```bash
cd mcp/clicd-mcp
npm install
```

## 配置

推荐使用本地配置文件（已被 `.gitignore` 忽略，不会提交到仓库）：

```bash
cp clicd-mcp.example.json clicd-mcp.local.json
# 编辑 clicd-mcp.local.json，填入各面板的 baseUrl 和 apiKey
```

`clicd-mcp.local.json`：

```json
{
  "defaultPanel": "独服",
  "panels": {
    "独服": {
      "baseUrl": "http://192.3.170.78:8999",
      "apiKey": "clicd_sk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
    },
    "绿云": {
      "baseUrl": "http://173.249.204.248:8999",
      "apiKey": "clicd_sk_yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy"
    }
  }
}
```

- `defaultPanel`：省略 `panel` 参数时使用哪台，缺省为配置里的第一台。
- 面板名称可以随便取（中文也行），工具返回里会带上 `panel` 字段。

也可以改用环境变量：

| 变量 | 说明 |
|---|---|
| `CLICD_BASE_URL` | 默认面板地址（覆盖本地文件里的默认面板） |
| `CLICD_API_KEY` | 默认面板的 API Key |
| `CLICD_PANELS` | 追加面板的 JSON，如 `{"绿云":{"baseUrl":"...","apiKey":"..."}}` |
| `CLICD_TIMEOUT_MS` | 普通请求超时，默认 120000 |
| `CLICD_CREATE_TIMEOUT_MS` | 建机/重装/快照超时，默认 1800000 |
| `CLICD_TASK_TIMEOUT_MS` | 等待后台任务超时，默认 900000 |
| `CLICD_INSECURE` | 设为 `1` 时接受自签名 HTTPS 证书 |

### 创建 API Key

在每台面板的「设置 → API 密钥」新建一个 Key，作用域填 `*`（MCP 需要建机、镜像、存储、路由等管理权限）。

> API Key 认证会绕过面板的隐藏入口（knock path），因此 MCP 不需要知道入口前缀。

## 接入 ZCode

用户级配置 `~/.zcode/cli/config.json`：

```json
{
  "mcp": {
    "servers": {
      "clicd": {
        "type": "stdio",
        "command": "C:/Program Files/nodejs/node.exe",
        "args": ["C:/Users/Administrator/Documents/GitHub/clicd/mcp/clicd-mcp/server.js"],
        "timeoutMs": 1800000
      }
    }
  }
}
```

凭据放在 `clicd-mcp.local.json` 里，不写进 ZCode 配置。配置完成后重启 ZCode 或新开会话，工具会以 `mcp__clicd__*` 的形式出现。

## 工具一览

所有工具都支持可选参数 `panel`（面板名称），省略则用默认面板。

| 工具 | 作用 |
|---|---|
| `clicd_list_panels` | 列出已配置的面板及默认面板 |
| `clicd_ping` | 验证面板地址和 API Key（省略 panel 时检查全部） |
| `clicd_list_all_images` | 列出全部已注册镜像及下载状态/进度 |
| `clicd_download_image` | 下载镜像（默认等到下载完成） |
| `clicd_set_image_enabled` | 启用/停用某个镜像 |
| `clicd_list_templates` | 列出**已下载且已启用**、可直接建机的镜像 |
| `clicd_list_containers` | 列出实例（可按 LXC/KVM、运行状态过滤） |
| `clicd_get_container` | 查看单台实例完整信息 |
| `clicd_create_container` | 创建一台 LXC 容器或 KVM 虚拟机 |
| `clicd_batch_create` | 批量创建多台实例 |
| `clicd_start_container` / `clicd_stop_container` / `clicd_restart_container` | 开关机/重启（等待任务完成） |
| `clicd_delete_container` | 删除实例及其磁盘（破坏性） |
| `clicd_reinstall_container` | 重装系统，可带 SSH 凭据和初始化脚本 |
| `clicd_reset_password` | 重置 SSH/管理员密码 |
| `clicd_get_usage` | 实时 CPU/内存/磁盘/网络占用 |
| `clicd_list_storage` | 存储池、容量、剩余空间 |
| `clicd_list_routing` | NAT 端口范围、公网 IPv4/IPv6 分配情况 |
| `clicd_host_info` | 宿主容量与实例统计 |
| `clicd_list_snapshots` / `clicd_create_snapshot` / `clicd_restore_snapshot` / `clicd_delete_snapshot` | 快照管理 |
| `clicd_list_tasks` / `clicd_wait_task` | 查看/等待后台任务 |
| `clicd_list_kvm_media` | 可挂载的 ISO 列表（`boot_media` / `extra_iso`） |

## 典型流程

**在一台新面板上开机器**（例如刚装好、还没有镜像的面板）：

1. `clicd_list_all_images`（`panel`）确认哪些镜像已注册但未下载；
2. `clicd_download_image`（`panel` + `template_id`）下载并等待完成；
3. `clicd_create_container`（`panel` + `name` + `template_id`）创建实例；
4. `clicd_list_containers`（`panel`）确认实例已出现。

**创建一台带初始化脚本的 Debian 12 LXC**：

```json
{
  "panel": "绿云",
  "name": "demo-lxc",
  "template_id": "debian-bookworm",
  "virtualization": "lxc",
  "vcpu": 1,
  "ram_mb": 512,
  "disk_gb": 10,
  "assign_nat": true,
  "init_script": "apt-get update && apt-get install -y htop curl"
}
```

**创建一台 Windows KVM 虚拟机**：

```json
{
  "panel": "独服",
  "name": "win-demo",
  "template_id": "custom-kvm-42e957647c",
  "virtualization": "kvm",
  "vcpu": 2,
  "ram_mb": 4096,
  "disk_gb": 40,
  "assign_nat": true,
  "port_mapping_count": 1
}
```

> `template_id` 先用 `clicd_list_templates`（可直接建机）或 `clicd_list_all_images`（含未下载）查询。创建时如果省略 `virtualization`，服务会根据模板自动判断 LXC/KVM。

## 冒烟测试

```bash
node test-client.mjs
```

脚本会打印工具列表、各面板连通性、以及每台面板上的实例数量。

## 安全说明

- `clicd-mcp.local.json` 含明文 API Key，已被 `.gitignore` 忽略；不要提交。
- 建议每台面板单独建一个 Key（如都叫 `mcp-agent`），便于随时在面板里禁用/删除。
- `clicd_delete_container`、`clicd_reinstall_container`、`clicd_restore_snapshot`、`clicd_delete_snapshot` 属于破坏性操作，工具定义里已标记 `destructiveHint`。
