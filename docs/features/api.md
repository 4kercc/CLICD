# API 集成

CLICD 对外推荐使用 `/api/v1` 接口。旧版未带版本号的接口主要用于 Web 面板和兼容场景，新接入请优先使用 `/api/v1`。

## 认证

API Key 可在“API 集成”页面创建和管理。请求时支持两种写法：

```bash
curl -H "X-API-Key: YOUR_API_KEY" https://panel.example.com/api/v1/containers
```

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" https://panel.example.com/api/v1/dashboard
```

## Python 示例

```python
import requests

BASE_URL = "https://panel.example.com"
API_KEY = "YOUR_API_KEY"

session = requests.Session()
session.headers.update({
    "X-API-Key": API_KEY,
    "Content-Type": "application/json",
})

resp = session.get(f"{BASE_URL}/api/v1/containers", timeout=15)
resp.raise_for_status()
containers = resp.json()

print(containers)
```

创建端口映射：

```python
import requests

BASE_URL = "https://panel.example.com"
API_KEY = "YOUR_API_KEY"
CONTAINER_ID = "example-vm"

payload = {
    "name": "web",
    "protocol": "tcp",
    "host_port": 18080,
    "container_port": 80,
}

resp = requests.post(
    f"{BASE_URL}/api/v1/containers/{CONTAINER_ID}/port-mappings",
    headers={"X-API-Key": API_KEY},
    json=payload,
    timeout=15,
)
resp.raise_for_status()
print(resp.json())
```

## 返回结构示例

容器列表：

```json
{
  "success": true,
  "data": [
    {
      "id": 5,
      "uuid": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
      "name": "example-vm",
      "status": "running",
      "ip": "10.0.3.25",
      "ipv6": "2001:db8:100::1005",
      "cpu_limit": 2,
      "memory_limit": 2048,
      "disk_limit": 20480,
      "traffic_limit": 107374182400,
      "expires_at": "2026-12-31 23:59:59"
    }
  ]
}
```

任务队列：

```json
{
  "success": true,
  "data": [
    {
      "id": "task-13",
      "type": "restart",
      "status": "running",
      "created_at": "2026-06-09T10:00:00+08:00"
    }
  ]
}
```

WebSSH 票据：

```json
{
  "success": true,
  "data": {
    "ticket": "***60秒有效票据***"
  }
}
```

## 常用接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/dashboard` | 控制面板统计 |
| GET | `/api/v1/host-info` | 主机资源 |
| GET | `/api/v1/containers` | 容器列表 |
| POST | `/api/v1/containers` | 创建容器 |
| POST | `/api/v1/containers/{id}/start` | 开机 |
| POST | `/api/v1/containers/{id}/stop` | 关机 |
| POST | `/api/v1/containers/{id}/restart` | 重启 |
| DELETE | `/api/v1/containers/{id}/delete` | 删除 |
| GET | `/api/v1/tasks` | 任务队列 |
| GET | `/api/v1/templates` | 模板列表 |
| GET | `/api/v1/images` | 镜像管理列表 |
| GET | `/api/v1/snapshots` | 快照总览 |
| GET | `/api/v1/security/alerts` | 安全告警 |
| GET | `/api/v1/audit-logs` | 操作日志 |
| GET | `/api/v1/api-keys` | API Key 列表 |

完整接口清单请以面板内“API 集成”页面为准。
