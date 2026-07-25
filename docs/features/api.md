# API 集成

CLICD 继续兼容旧版 `/api` 接口，已有对接无需修改。新接入推荐使用 `/api/v1` 接口，下面的清单均为 v1；容器列表推荐 `GET /api/v1/containers`。

## 认证

API Key 可在“API 集成”页面创建和管理。请求时支持两种写法：

```bash
curl -H "X-API-Key: YOUR_API_KEY" https://panel.example.com/api/v1/containers
```

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" https://panel.example.com/api/v1/dashboard
```

## 响应结构

所有接口保持统一响应包裹：

```json
{
  "success": true,
  "message": "OK",
  "data": {}
}
```

对接时建议只读取业务所需字段。新增能力会优先追加可选字段，不会要求已有插件改掉现有字段名。

## 创建与重装

创建容器、批量创建、重装和批量重装已支持 NAT、公网 IPv4、IPv6 混合网络，以及 Linux SSH 登录方式配置。公网 IPv4/IPv6 地址池可通过 `GET /api/v1/routing` 查看，并可通过 `PUT /api/v1/routing` 更新。

创建容器示例：

```json
{
  "name": "demo-lxc-01",
  "virtualization": "lxc",
  "template_id": "debian-bookworm",
  "vcpu": 1,
  "ram_mb": 512,
  "disk_gb": 10,
  "assign_nat": true,
  "port_mapping_count": 2,
  "assign_ipv4": false,
  "ipv4_count": 1,
  "public_ipv4s": [],
  "assign_ipv6": true,
  "ipv6_count": 1,
  "ipv6_addresses": [],
  "ssh_auth_mode": "auto_password",
  "ssh_password": "",
  "ssh_public_key": "",
  "expires_at": "",
  "network_down_mbps": 100,
  "network_up_mbps": 50,
  "io_read_mbps": 120,
  "io_write_mbps": 80
}
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `assign_nat` | 是否分配 NAT 端口映射；不传时保持默认 NAT 行为。 |
| `assign_ipv4` | 是否分配公网 IPv4。 |
| `ipv4_count` | 自动分配公网 IPv4 数量。 |
| `public_ipv4s` | 指定公网 IPv4 地址列表。 |
| `assign_ipv6` | 是否分配 IPv6。 |
| `ipv6_count` | 自动分配 IPv6 数量。 |
| `ipv6_addresses` | 指定 IPv6 地址列表。 |
| `ssh_auth_mode` | Linux 创建支持 `auto_password`、`password`、`key`；重装额外支持 `keep`。 |
| `ssh_password` | `password` 模式下的自定义密码；8-64 位，至少包含字母和数字，不能包含空白字符。 |
| `ssh_public_key` | `key` 模式下的一行 SSH 公钥。 |
| `network_down_mbps` | 可选；容器下行/下载带宽限制，单位 Mbps，`0` 表示不限制。 |
| `network_up_mbps` | 可选；容器上行/上传带宽限制，单位 Mbps，`0` 表示不限制。 |
| `io_read_mbps` | 可选；磁盘读取限速，单位 MB/s，`0` 表示不限制。 |
| `io_write_mbps` | 可选；磁盘写入限速，单位 MB/s，`0` 表示不限制。 |
| `network_bw_mbps` | 兼容旧字段；同时设置上下行对称带宽，新接入推荐使用拆分字段。 |
| `io_speed_mbps` | 兼容旧字段；同时设置读写对称 IO 限速，新接入推荐使用拆分字段。 |

重装示例：

```json
{
  "template_id": "debian-bookworm",
  "ssh_auth_mode": "keep",
  "ssh_password": "",
  "ssh_public_key": ""
}
```

`keep` 仅用于重装，表示沿用当前 SSH 密码。Windows KVM 镜像会忽略 Linux SSH 公钥相关字段。

## 资源限制与流量限制

`PUT /api/v1/containers/{id}/resource-limit` 支持按字段局部更新；未传的字段保持不变。

```json
{
  "vcpu": 2,
  "ram_mb": 1024,
  "network_down_mbps": 100,
  "network_up_mbps": 50,
  "io_read_mbps": 120,
  "io_write_mbps": 80
}
```

旧版 `network_bw_mbps` 和 `io_speed_mbps` 仍可用，分别表示上下行对称带宽和读写对称 IO 限速。新接入建议使用拆分字段，以便分别控制下载/上传和读取/写入。

`PUT /api/v1/containers/{id}/traffic-limit` 请求体：

```json
{
  "traffic_mode": "total",
  "monthly_traffic_gb": 1024,
  "traffic_in_gb": 0,
  "traffic_out_gb": 0
}
```

| 字段 | 说明 |
| --- | --- |
| `traffic_mode` | 流量限制模式；常用 `total` 表示总量限制，`split` 表示入站/出站分别限制。 |
| `monthly_traffic_gb` | `total` 模式下的月总流量额度，单位 GB；`0` 表示不限制。 |
| `traffic_in_gb` | `split` 模式下的月入站额度，单位 GB；`0` 表示不限制。 |
| `traffic_out_gb` | `split` 模式下的月出站额度，单位 GB；`0` 表示不限制。 |

## 容器防火墙

容器防火墙通过 `GET /api/v1/containers/{id}/firewall` 读取，通过 `PUT /api/v1/containers/{id}/firewall` 更新。容器运行中更新时会立即应用规则。

更新示例：

```json
{
  "enabled": true,
  "default_action": "DROP",
  "rules": [
    {
      "direction": "in",
      "protocol": "tcp",
      "action": "ACCEPT",
      "network": "ipv4",
      "source_ip": "203.0.113.0/24",
      "port": "22,80,443",
      "description": "allow admin and web"
    }
  ]
}
```

| 字段 | 说明 |
| --- | --- |
| `enabled` | 是否启用容器防火墙。 |
| `default_action` | 默认动作：`ACCEPT` 或 `DROP`。 |
| `rules[].id` | 可选；新规则可省略，后端会自动生成。 |
| `rules[].direction` | 方向：`in` 或 `out`。 |
| `rules[].protocol` | 协议：`tcp`、`udp`、`icmp` 或 `all`。 |
| `rules[].action` | 动作：`ACCEPT` 或 `DROP`。 |
| `rules[].network` | 网络类型：`ipv4`、`ipv6` 或 `all`。 |
| `rules[].source_ip` | 可选；源 IP、CIDR 或地址范围。 |
| `rules[].port` | 可选；仅 `tcp`/`udp` 支持，可写 `22`、`80,443` 或 `8000-9000`。 |
| `rules[].description` | 可选备注。 |

## API Key 创建与更新

`POST /api/v1/api-keys` 和 `PATCH /api/v1/api-keys/{id}` 使用相同的字段结构。创建时 `name` 必填；更新时根据需要覆盖字段。

```json
{
  "name": "Automation",
  "ip_whitelist": "198.51.100.23,203.0.113.0/24",
  "scopes": ["dashboard:read", "container:read", "container:power"],
  "expires_at": "2026-12-31 23:59:59",
  "disabled": false,
  "container_uuids": ["00000000-0000-4000-8000-000000000005"]
}
```

| 字段 | 说明 |
| --- | --- |
| `name` | API Key 名称；创建时必填。 |
| `ip_whitelist` | 可选；允许的来源 IP/CIDR，多个值用逗号分隔；空值表示不限制。 |
| `scopes` | 可选；权限范围。省略时使用默认只读范围，传 `*` 表示全部权限。 |
| `expires_at` | 可选；过期时间，空值表示不过期。 |
| `disabled` | 是否禁用该 Key。 |
| `container_uuids` | 可选；限制该 Key 只能访问指定容器。 |

## 面板访问来源策略

`GET /api/v1/access-policy` 读取面板访问白名单，`PUT /api/v1/access-policy` 更新策略。两者均需要 `admin:access` 权限。策略覆盖面板页面、登录入口和全部 API。

```json
{
  "enabled": true,
  "allowed_sources": [
    "203.0.113.10",
    "192.168.1.0/24",
    "2001:db8::/32"
  ],
  "trusted_proxies": [
    "127.0.0.1"
  ]
}
```

`allowed_sources` 和 `trusted_proxies` 均支持 IPv4、IPv6 及 CIDR。只有直接连接来源命中 `trusted_proxies` 时，后端才会使用 `X-Forwarded-For`、`X-Real-IP` 或 `CF-Connecting-IP`；其他客户端伪造这些请求头不会绕过白名单。启用策略时至少要配置一个允许来源，且接口会拒绝排除当前管理来源的配置。本机回环直连保留为 CLI/SSH 故障恢复通道。

## Python 示例

获取容器列表：

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
print(resp.json())
```

创建端口映射：

```python
import requests

BASE_URL = "https://panel.example.com"
API_KEY = "YOUR_API_KEY"
CONTAINER_ID = "example-vm"

payload = {
    "protocol": "tcp",
    "host_port": 18080,
    "container_port": 80,
    "description": "web",
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

## 接口清单

### 总览

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/dashboard` | 控制面板统计 |
| GET | `/api/v1/host-info` | 主机资源 |
| GET | `/api/v1/host-report` | 主机巡检报告 |
| GET | `/api/v1/routing` | NAT/IPv4/IPv6 路由 |
| PUT | `/api/v1/routing` | 更新公网 IPv4/IPv6 池 |
| POST | `/api/v1/routing/ipv4-scan` | 扫描公网 IPv4 段 |
| GET | `/api/v1/ipv6/status` | IPv6 状态 |
| GET | `/api/v1/tasks` | 任务队列 |
| DELETE | `/api/v1/tasks/{task_id}` | 删除任务 |

### 容器

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/containers` | 容器列表（推荐） |
| GET | `/api/v1/containers/list` | 容器列表兼容 GET 写法 |
| POST | `/api/v1/containers/list` | 容器列表兼容 POST 写法 |
| POST | `/api/v1/containers` | 创建容器 |
| GET | `/api/v1/containers/{id\|uuid\|name}` | 容器详情 |
| POST | `/api/v1/containers/{id}/start` | 开机 |
| POST | `/api/v1/containers/{id}/stop` | 关机 |
| POST | `/api/v1/containers/{id}/restart` | 重启 |
| POST | `/api/v1/containers/{id}/reinstall` | 重装 |
| DELETE | `/api/v1/containers/{id}/delete` | 删除 |
| GET | `/api/v1/containers/{id}/usage` | 资源用量 |
| GET | `/api/v1/containers/{id}/traffic` | 流量统计 |
| POST | `/api/v1/containers/{id}/traffic-reset` | 重置流量 |
| PUT | `/api/v1/containers/{id}/traffic-limit` | 调整流量限制 |
| PUT | `/api/v1/containers/{id}/resource-limit` | 调整资源限制 |
| PUT | `/api/v1/containers/{id}/expiry` | 调整到期时间 |
| POST | `/api/v1/containers/{id}/reset-password` | 重置 SSH 密码 |
| POST | `/api/v1/containers/{id}/ipv6` | 分配 IPv6 |

### 端口与快照

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/containers/{id}/random-port` | 随机可用端口；可传 `host_ip` 查询指定宿主机 IP |
| POST | `/api/v1/containers/{id}/port-mappings` | 添加端口映射 |
| PUT | `/api/v1/containers/{id}/port-mappings/{index}` | 更新端口映射 |
| DELETE | `/api/v1/containers/{id}/port-mappings/{index}` | 删除端口映射 |
| GET | `/api/v1/containers/{id}/firewall` | 获取容器防火墙设置 |
| PUT | `/api/v1/containers/{id}/firewall` | 更新容器防火墙设置 |
| GET | `/api/v1/snapshots` | 快照总览 |
| GET | `/api/v1/containers/{id}/snapshots` | 容器快照 |
| POST | `/api/v1/containers/{id}/snapshots` | 创建快照 |
| DELETE | `/api/v1/containers/{id}/snapshots/{snapshot_id}` | 删除快照 |
| POST | `/api/v1/containers/{id}/snapshots/{snapshot_id}/restore` | 恢复快照 |
| POST | `/api/v1/containers/{id}/snapshots/schedule` | 计划快照 |
| PUT | `/api/v1/containers/{id}/snapshots/quota` | 快照配额 |

### 平台管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/templates` | 模板列表 |
| GET | `/api/v1/images` | 镜像管理列表 |
| GET | `/api/v1/images/enabled` | 已启用且已下载的镜像；支持 `type=lxc\|kvm` |
| POST | `/api/v1/images/custom` | 添加第三方 LXC/KVM 镜像源 |
| DELETE | `/api/v1/images/custom` | 移除第三方 LXC/KVM 镜像源及缓存 |
| POST | `/api/v1/images/download` | 下载镜像 |
| POST | `/api/v1/images/cancel` | 取消镜像下载 |
| DELETE | `/api/v1/images/delete` | 删除镜像缓存 |
| PUT | `/api/v1/images/toggle` | 启用/禁用镜像 |
| GET | `/api/v1/security/alerts` | 安全告警 |
| POST | `/api/v1/security/check` | 立即安全检查 |
| GET | `/api/v1/security/logs?container={name}` | 安全连接日志 |
| GET | `/api/v1/security/summary` | 安全汇总 |
| GET | `/api/v1/security/settings` | 安全设置 |
| PUT | `/api/v1/security/settings` | 更新安全设置 |
| GET | `/api/v1/swap` | Swap 信息 |
| POST | `/api/v1/swap` | 调整 Swap |
| GET | `/api/v1/language` | 当前面板语言 |
| POST/PUT | `/api/v1/language` | 更新面板语言 |
| GET | `/api/v1/ssl` | SSL 设置（需管理员权限 / `admin:access`） |
| PUT | `/api/v1/ssl` | 更新 SSL 设置（需管理员权限 / `admin:access`） |
| GET | `/api/v1/webssh-origins` | WebSSH Origin 白名单（需管理员权限 / `admin:access`） |
| PUT | `/api/v1/webssh-origins` | 更新 WebSSH Origin 白名单（需管理员权限 / `admin:access`） |
| POST | `/api/v1/batch-create` | 批量创建容器 |
| POST | `/api/v1/batch-action` | 批量开关机/删除/重装 |
| POST | `/api/v1/ssh-ticket` | 创建 WebSSH 票据 |
| POST | `/api/v1/vnc-ticket` | 创建 WebVNC 票据 |

### 账号与日志

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/v1/sub-user/create` | 创建子用户链接 |
| GET | `/api/v1/sub-users` | 子用户列表 |
| POST | `/api/v1/sub-users/{id}/rotate-password` | 轮换子用户密码 |
| GET | `/api/v1/sub-users/{id}/audit-logs` | 子用户操作日志 |
| GET | `/api/v1/sub-users/{id}/login-logs` | 子用户登录日志 |
| GET | `/api/v1/audit-logs` | 操作日志 |
| GET | `/api/v1/login-logs` | 登录日志 |
| GET | `/api/v1/api-keys` | API Key 列表 |
| POST | `/api/v1/api-keys` | 创建 API Key |
| PATCH | `/api/v1/api-keys/{id}` | 更新 API Key |
| DELETE | `/api/v1/api-keys/{id}` | 删除 API Key |

## 返回样例

以下样例按接口路径分组。真实环境中的资源数值、任务 ID、容器 ID、时间、IP 和密钥会不同，示例中的密码、票据和 API Key 均已脱敏。

### 总览

```json
{
  "GET /api/v1/dashboard": {
    "success": true,
    "data": {
      "running": 31,
      "stopped": 0,
      "total_containers": 31
    }
  },
  "GET /api/v1/host-info": {
    "success": true,
    "data": {
      "cpu": { "cores": 8, "usage_pct": 1.16 },
      "ram": { "total_mb": 31825, "used_mb": 1275, "free_mb": 30550 },
      "disk": { "total_gb": 1750.49, "used_gb": 123.98, "free_gb": 1626.51 },
      "network": {
        "public_ipv4": "203.0.113.10",
        "public_ipv4_interface": "eth0",
        "public_ipv6": "2001:db8:100::2",
        "public_ipv6_interface": "eth0"
      },
      "load": { "load1": 0.01, "load5": 0.03, "load15": 0.01 }
    }
  },
  "GET /api/v1/host-report": {
    "success": true,
    "data": {
      "generated_at": "2026-06-12 10:00:00",
      "summary": { "status": "ok", "warnings": 0 },
      "host": { "hostname": "node-1", "kernel": "6.8.0" },
      "resources": { "cpu_cores": 8, "ram_total_mb": 31825, "disk_total_gb": 1750.49 },
      "network": { "public_ipv4": "203.0.113.10", "public_ipv6": "2001:db8:100::2" }
    }
  },
  "GET /api/v1/routing": {
    "success": true,
    "data": {
      "nat4": { "used": 62, "remaining": "45474", "total": "45536" },
      "ipv4": { "used": 1, "remaining": "3", "total": "4" },
      "ipv6": { "used": 31, "remaining": "large", "total": "large" },
      "public_ipv4_addresses": [
        { "address": "203.0.113.10", "interface": "eth0", "prefix_len": 32, "gateway": "203.0.113.1" }
      ],
      "ipv4_assignments": [
        { "container_id": 5, "container_name": "example-vm", "address": "203.0.113.10", "interface": "eth0", "prefix_len": 32, "gateway": "203.0.113.1" }
      ],
      "nat4_mappings": [
        { "container_id": 5, "container_name": "example-vm", "status": "running", "ip": "10.0.0.10", "host_port": 22004, "container_port": 22, "protocol": "tcp" }
      ],
      "ipv6_assignments": [
        { "container_id": 5, "container_name": "example-vm", "address": "2001:db8:100::1005", "prefix_len": 64, "interface": "eth0" }
      ]
    }
  },
  "PUT /api/v1/routing": {
    "success": true,
    "data": {
      "ipv4": { "used": 1, "remaining": "3", "total": "4" },
      "public_ipv4_addresses": [
        { "address": "203.0.113.10", "interface": "eth0", "prefix_len": 32, "gateway": "203.0.113.1" }
      ],
      "ipv6_prefixes": [
        { "interface": "eth0", "address": "2001:db8:100::2", "prefix": "2001:db8:100::/64", "prefix_len": 64, "gateway": "2001:db8:100::1" }
      ]
    }
  },
  "POST /api/v1/routing/ipv4-scan": {
    "success": true,
    "data": [
      { "address": "203.0.113.10", "interface": "eth0", "prefix_len": 32, "gateway": "203.0.113.1", "status": "available", "usable": true, "reason": "" }
    ]
  },
  "GET /api/v1/ipv6/status": {
    "success": true,
    "data": {
      "available": true,
      "reachable": true,
      "reason": "usable public IPv6 prefix detected",
      "prefixes": [
        { "interface": "eth0", "address": "2001:db8:100::2", "prefix": "2001:db8:100::/64", "prefix_len": 64, "gateway": "2001:db8:100::1" }
      ]
    }
  },
  "GET /api/v1/tasks": {
    "success": true,
    "data": []
  },
  "DELETE /api/v1/tasks/{task_id}": {
    "success": true,
    "message": "Task deleted"
  }
}
```

### 容器

```json
{
  "GET /api/v1/containers": {
    "success": true,
    "data": [
      {
        "id": 5,
        "uuid": "00000000-0000-4000-8000-000000000005",
        "name": "example-vm",
        "virtualization": "lxc",
        "template": "debian-bullseye",
        "vcpu": 1,
        "ram_mb": 512,
        "disk_gb": 10,
        "network_down_mbps": 100,
        "network_up_mbps": 50,
        "io_read_mbps": 120,
        "io_write_mbps": 80,
        "status": "running",
        "ip": "10.0.0.10",
        "ipv6": "2001:db8:100::1005",
        "ssh_port": 22004,
        "ssh_password": "***",
        "port_mappings": [
          { "container_port": 22, "host_port": 22004, "protocol": "tcp", "description": "SSH" },
          { "container_port": 20000, "host_port": 20000, "protocol": "tcp", "description": "Port-20000" }
        ]
      }
    ]
  },
  "GET /api/v1/containers/list": {
    "success": true,
    "data": [
      { "id": 5, "uuid": "00000000-0000-4000-8000-000000000005", "name": "example-vm", "status": "running", "ip": "10.0.0.10" }
    ]
  },
  "POST /api/v1/containers/list": {
    "success": true,
    "data": [
      { "id": 5, "uuid": "00000000-0000-4000-8000-000000000005", "name": "example-vm", "status": "running", "ip": "10.0.0.10" }
    ]
  },
  "POST /api/v1/containers": {
    "success": true,
    "message": "Container created successfully"
  },
  "GET /api/v1/containers/{id|uuid|name}": {
    "success": true,
    "data": {
      "id": 5,
      "uuid": "00000000-0000-4000-8000-000000000005",
      "name": "example-vm",
      "status": "running",
      "ip": "10.0.0.10",
      "ipv6": "2001:db8:100::1005",
      "ssh_port": 22004,
      "ssh_password": "***",
      "policy_blocked": false
    }
  },
  "POST /api/v1/containers/{id}/start": {
    "success": true,
    "message": "Task queued",
    "data": { "task_id": "task-10", "container_name": "example-vm", "status": "pending", "action": "start" }
  },
  "POST /api/v1/containers/{id}/stop": {
    "success": true,
    "message": "Task queued",
    "data": { "task_id": "task-10", "container_name": "example-vm", "status": "pending", "action": "stop" }
  },
  "POST /api/v1/containers/{id}/restart": {
    "success": true,
    "message": "Task queued",
    "data": { "task_id": "task-10", "container_name": "example-vm", "status": "pending", "action": "restart" }
  },
  "POST /api/v1/containers/{id}/reinstall": {
    "success": true,
    "message": "Task queued",
    "data": { "task_id": "task-10", "container_name": "example-vm", "status": "pending", "action": "reinstall" }
  },
  "DELETE /api/v1/containers/{id}/delete": {
    "success": true,
    "message": "Task queued",
    "data": { "task_id": "task-10", "container_name": "example-vm", "status": "pending", "action": "delete" }
  },
  "GET /api/v1/containers/{id}/usage": {
    "success": true,
    "data": {
      "cpu_usage_pct": 0,
      "cpu_usage_usec": 3908852,
      "memory_usage_bytes": 29331456,
      "disk_usage_bytes": 515100672,
      "network_rx_bytes": 131232,
      "network_tx_bytes": 16828,
      "load1": 0.1,
      "load5": 0.06,
      "load15": 0.01
    }
  },
  "GET /api/v1/containers/{id}/traffic": {
    "success": true,
    "data": {
      "mode": "total",
      "limit_gb": 1024,
      "in_limit_gb": 0,
      "out_limit_gb": 0,
      "total_used_bytes": 142082,
      "rx_used_bytes": 127212,
      "tx_used_bytes": 14870,
      "used_pct": 0,
      "reset_date": "2026-06"
    }
  },
  "POST /api/v1/containers/{id}/traffic-reset": {
    "success": true,
    "message": "Traffic reset"
  },
  "PUT /api/v1/containers/{id}/traffic-limit": {
    "success": true,
    "message": "Traffic limit updated"
  },
  "PUT /api/v1/containers/{id}/resource-limit": {
    "success": true,
    "message": "Resource limits updated"
  },
  "PUT /api/v1/containers/{id}/expiry": {
    "success": true,
    "message": "Expiry updated"
  },
  "POST /api/v1/containers/{id}/reset-password": {
    "success": true,
    "message": "SSH password reset successfully",
    "data": { "password": "***" }
  },
  "POST /api/v1/containers/{id}/ipv6": {
    "success": true,
    "message": "IPv6 assigned",
    "data": { "id": 5, "name": "example-vm", "ipv6": "2001:db8:100::1005" }
  }
}
```

### 端口与快照

```json
{
  "GET /api/v1/containers/{id}/random-port?host_ip=203.0.113.10": {
    "success": true,
    "data": { "port": 61320 }
  },
  "POST /api/v1/containers/{id}/port-mappings": {
    "success": true,
    "data": [
      { "container_port": 22, "host_port": 22004, "protocol": "tcp", "description": "SSH" },
      { "container_port": 8080, "host_port": 61320, "protocol": "tcp", "description": "HTTP" }
    ]
  },
  "PUT /api/v1/containers/{id}/port-mappings/{index}": {
    "success": true,
    "data": [
      { "container_port": 8081, "host_port": 61320, "protocol": "tcp", "description": "HTTP" }
    ]
  },
  "DELETE /api/v1/containers/{id}/port-mappings/{index}": {
    "success": true,
    "data": []
  },
  "GET /api/v1/containers/{id}/firewall": {
    "success": true,
    "data": {
      "enabled": true,
      "default_action": "DROP",
      "rules": [
        { "id": "a1b2c3d4", "direction": "in", "protocol": "tcp", "action": "ACCEPT", "network": "ipv4", "source_ip": "203.0.113.0/24", "port": "22,80,443", "description": "allow admin and web" }
      ]
    }
  },
  "PUT /api/v1/containers/{id}/firewall": {
    "success": true,
    "message": "Firewall updated",
    "data": { "enabled": true, "default_action": "DROP", "rules": [] }
  },
  "GET /api/v1/snapshots": {
    "success": true,
    "data": null
  },
  "GET /api/v1/containers/{id}/snapshots": {
    "success": true,
    "data": {
      "quota": 1,
      "schedule": { "enabled": false, "interval_hours": 0, "last_run": "", "next_run": "", "time": "", "created_by": "" },
      "snapshots": []
    }
  },
  "POST /api/v1/containers/{id}/snapshots": {
    "success": true,
    "data": {
      "id": "snap-20260608-001",
      "container_id": 5,
      "container_name": "example-vm",
      "created_at": "2026-06-08 16:00:00",
      "created_by": "api:Automation",
      "scheduled": false,
      "size_bytes": 10485760
    }
  },
  "DELETE /api/v1/containers/{id}/snapshots/{snapshot_id}": {
    "success": true,
    "message": "Snapshot deleted"
  },
  "POST /api/v1/containers/{id}/snapshots/{snapshot_id}/restore": {
    "success": true,
    "message": "Snapshot restored"
  },
  "POST /api/v1/containers/{id}/snapshots/schedule": {
    "success": true,
    "data": {
      "container": { "id": 5, "name": "example-vm", "snapshot_schedule_enabled": true, "snapshot_schedule_interval_hours": 24, "snapshot_schedule_time": "03:00" }
    }
  },
  "PUT /api/v1/containers/{id}/snapshots/quota": {
    "success": true,
    "data": {
      "quota": 2,
      "container": { "id": 5, "name": "example-vm", "snapshot_limit": 2 }
    }
  }
}
```

### 平台管理

```json
{
  "GET /api/v1/templates": {
    "success": true,
    "data": [
      { "id": "ubuntu-noble", "name": "Ubuntu 24.04", "distro": "ubuntu", "release": "noble", "arch": "amd64", "description": "Ubuntu 24.04 LTS" },
      { "id": "debian-bookworm", "name": "Debian 12", "distro": "debian", "release": "bookworm", "arch": "amd64", "description": "Debian 12 (Bookworm)" }
    ]
  },
  "GET /api/v1/images": {
    "success": true,
    "data": [
      { "id": "ubuntu-noble", "name": "Ubuntu 24.04", "type": "lxc", "downloaded": true, "enabled": true, "downloading": false, "progress": 0, "size_bytes": 135005452 }
    ]
  },
  "GET /api/v1/images/enabled?type=lxc": {
    "success": true,
    "data": [
      { "id": "ubuntu-noble", "name": "Ubuntu 24.04", "distro": "ubuntu", "release": "noble", "arch": "amd64", "variant": "default", "description": "Ubuntu 24.04 LTS", "type": "lxc" }
    ]
  },
  "POST /api/v1/images/download": {
    "success": true,
    "message": "Already downloaded"
  },
  "POST /api/v1/images/cancel": {
    "success": true,
    "message": "Cancel requested"
  },
  "DELETE /api/v1/images/delete": {
    "success": true,
    "message": "Deleted"
  },
  "PUT /api/v1/images/toggle": {
    "success": true,
    "message": "OK"
  },
  "GET /api/v1/security/alerts": {
    "success": true,
    "data": []
  },
  "POST /api/v1/security/check": {
    "success": true,
    "message": "Security check completed"
  },
  "GET /api/v1/security/logs?container={name}": {
    "success": true,
    "data": []
  },
  "GET /api/v1/security/summary": {
    "success": true,
    "data": { "critical": 0, "high": 0, "medium": 0, "low": 0, "total_alerts": 0 }
  },
  "GET /api/v1/security/settings": {
    "success": true,
    "data": { "auto_shutdown": false }
  },
  "PUT /api/v1/security/settings": {
    "success": true,
    "data": { "auto_shutdown": false }
  },
  "GET /api/v1/swap": {
    "success": true,
    "data": { "total_mb": 16383, "used_mb": 0, "free_mb": 16383, "enabled": true, "swap_file": "/swapfile" }
  },
  "POST /api/v1/swap": {
    "success": true,
    "message": "SWAP 已调整为 16384 MB",
    "data": { "total_mb": 16383, "used_mb": 0, "free_mb": 16383, "enabled": true, "swap_file": "/swapfile" }
  },
  "GET /api/v1/language": {
    "success": true,
    "data": { "language": "zh" }
  },
  "PUT /api/v1/language": {
    "success": true,
    "data": { "language": "en" }
  },
  "GET /api/v1/ssl": {
    "success": true,
    "data": { "enabled": true, "mode": "self-signed", "target": "panel.example.com", "detected_host": "panel.example.com", "needs_restart": false }
  },
  "PUT /api/v1/ssl": {
    "success": true,
    "message": "SSL settings saved",
    "data": { "enabled": true, "mode": "self-signed", "target": "panel.example.com", "needs_restart": true }
  },
  "GET /api/v1/webssh-origins": {
    "success": true,
    "data": { "origins": ["https://panel.example.com"], "current_origin": "https://panel.example.com" }
  },
  "PUT /api/v1/webssh-origins": {
    "success": true,
    "message": "Origin allowlist saved",
    "data": { "origins": ["https://panel.example.com"], "current_origin": "https://panel.example.com" }
  },
  "POST /api/v1/batch-create": {
    "success": true,
    "data": ["task-12"]
  },
  "POST /api/v1/batch-action": {
    "success": true,
    "data": ["task-13"]
  },
  "POST /api/v1/ssh-ticket": {
    "success": true,
    "data": { "ticket": "***60秒有效票据***" }
  },
  "POST /api/v1/vnc-ticket": {
    "success": true,
    "data": { "ticket": "***60秒有效票据***" }
  }
}
```

### 账号与日志

```json
{
  "POST /api/v1/sub-user/create": {
    "success": true,
    "message": "Sub-user created",
    "data": {
      "id": "sub-xxxxxxxx",
      "username": "user-xxxxxxxx",
      "password": "***",
      "container_names": ["example-vm"],
      "access_code": "********",
      "created_at": "2026-06-08 16:00:00"
    }
  },
  "GET /api/v1/sub-users": {
    "success": true,
    "data": []
  },
  "POST /api/v1/sub-users/{id}/rotate-password": {
    "success": true,
    "data": { "username": "user-xxxxxxxx", "password": "***", "access_code": "********" }
  },
  "GET /api/v1/sub-users/{id}/audit-logs": {
    "success": true,
    "data": []
  },
  "GET /api/v1/sub-users/{id}/login-logs": {
    "success": true,
    "data": []
  },
  "GET /api/v1/audit-logs": {
    "success": true,
    "data": [
      { "time": "2026-06-08 15:44:40", "action": "apikey.create", "target": "Test", "detail": "scopes=*", "user": "admin", "success": true }
    ]
  },
  "GET /api/v1/login-logs": {
    "success": true,
    "data": [
      { "time": "2026-06-08 08:24:00 UTC", "username": "admin", "ip": "198.51.100.23", "user_agent": "Mozilla/5.0 ...", "success": true }
    ]
  },
  "GET /api/v1/api-keys": {
    "success": true,
    "data": [
      { "id": "c271023f", "name": "Test", "prefix": "clicd_sk_dd9d...", "ip_whitelist": "", "created_at": "2026-06-08 15:44:40", "last_used": "2026-06-08 15:46:10", "scopes": ["*"], "expires_at": "", "disabled": false, "container_uuids": [], "last_used_ip": "198.51.100.23" }
    ]
  },
  "POST /api/v1/api-keys": {
    "success": true,
    "message": "API key created. Save this key now - it won't be shown again.",
    "data": { "id": "a1b2c3d4", "name": "Automation", "key": "clicd_sk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "prefix": "clicd_sk_xxxx...", "ip_whitelist": "198.51.100.23", "scopes": ["dashboard:read", "container:read"], "expires_at": "2026-12-31 23:59:59", "disabled": false, "container_uuids": ["00000000-0000-4000-8000-000000000005"] }
  },
  "PATCH /api/v1/api-keys/{id}": {
    "success": true,
    "data": { "id": "a1b2c3d4", "name": "Automation", "prefix": "clicd_sk_xxxx...", "scopes": ["dashboard:read", "container:read"], "expires_at": "2026-12-31 23:59:59", "disabled": false, "container_uuids": ["00000000-0000-4000-8000-000000000005"] }
  },
  "DELETE /api/v1/api-keys/{id}": {
    "success": true,
    "message": "API key deleted"
  }
}
```
