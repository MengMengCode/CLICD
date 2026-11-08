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

大多数 JSON 接口返回 `success`，以及可选的 `message` 和 `data`。先检查 HTTP 状态码。备份下载返回归档文件。

```json
{
  "success": true,
  "message": "OK",
  "data": {}
}
```

对接时建议只读取业务所需字段。新增能力会优先追加可选字段，不会要求已有插件改掉现有字段名。

## 创建与重装

创建容器和批量创建可以组合配置 NAT、公网 IPv4、IPv6。重装和批量重装用于更换系统镜像，并可设置 Linux SSH 登录方式。公网 IPv4/IPv6 地址池可通过 `GET /api/v1/routing` 查看，并可通过 `PUT /api/v1/routing` 更新。

创建容器示例：

```json
{
  "name": "demo-lxc-01",
  "virtualization": "lxc",
  "privileged": false,
  "nesting": false,
  "template_id": "debian-bookworm",
  "vcpu": 1,
  "ram_mb": 512,
  "disk_gb": 10,
  "assign_nat": true,
  "port_mapping_count": 2,
  "assign_ipv4": false,
  "ipv4_count": 1,
  "public_ipv4s": [],
  "assign_ipv6": false,
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

`privileged` 和 `nesting` 仅适用于 LXC，省略时都为 `false`。`nesting=true` 会启用 Docker、LXC-in-LXC 和部分 systemd 工作负载需要的嵌套能力；`privileged=true` 会创建特权容器，降低隔离强度，只允许管理员创建，适合可信工作负载。已有实例不能通过 API 在线切换这两种模式。KVM 请求中将任一参数设为 `true` 会返回 `400`，非管理员请求特权 LXC 会返回 `403`。

批量创建时，将相同参数放在对应的 `containers[]` 项中。容器列表和详情响应使用 `lxc_privileged`、`lxc_nesting` 表示实际状态；这两个字段采用省略空值的 JSON 编码，关闭状态可能不会出现在响应中。

KVM 创建的通用资源下限为 `vcpu >= 1`、`ram_mb >= 1024`、`disk_gb >= 10`；镜像自身声明的更高磁盘要求仍然优先。

字段说明：

| 字段 | 说明 |
| --- | --- |
| `virtualization` | `lxc` 或 `kvm`。`privileged` 和 `nesting` 只对 `lxc` 生效。 |
| `privileged` | 可选布尔值，默认 `false`。仅 LXC 支持；创建特权容器需要管理员权限，且会降低隔离强度。 |
| `nesting` | 可选布尔值，默认 `false`。仅 LXC 支持；启用 Docker/LXC-in-LXC 和 systemd 所需的嵌套能力。 |
| `template_id` | 已下载并启用的镜像 ID。KVM 镜像可能声明高于通用下限的最小磁盘。 |
| `vcpu` / `ram_mb` / `disk_gb` | vCPU、内存（MB）和磁盘（GB）。KVM 通用下限分别为 1、1024 和 10。 |
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

创建成功后，通过 `GET /api/v1/containers` 或 `GET /api/v1/containers/{id|uuid|name}` 查看实际状态。LXC 的特权和嵌套状态分别读取 `lxc_privileged`、`lxc_nesting`；关闭状态因 JSON 省略空值可能缺省，客户端应按 `false` 处理缺省值。

重装示例：

```json
{
  "template_id": "debian-bookworm",
  "ssh_auth_mode": "keep",
  "ssh_password": "",
  "ssh_public_key": ""
}
```

`keep` 仅用于重装，表示沿用当前 SSH 密码。Windows KVM 镜像会忽略 Linux SSH 公钥相关字段。使用手动安装 ISO 创建 KVM 时，将镜像 ID 作为 `template_id`，创建后通过控制台安装。

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
| `traffic_mode` | 流量限制模式；常用 `total` 表示总量限制，`in_out` 表示入站/出站分别限制。 |
| `monthly_traffic_gb` | `total` 模式下的月总流量额度，单位 GB；`0` 表示不限制。 |
| `traffic_in_gb` | `in_out` 模式下的月入站额度，单位 GB；`0` 表示不限制。 |
| `traffic_out_gb` | `in_out` 模式下的月出站额度，单位 GB；`0` 表示不限制。 |

## 容器防火墙

容器防火墙通过 `GET /api/v1/containers/{id}/firewall` 读取，通过 `PUT /api/v1/containers/{id}/firewall` 更新。容器运行中更新时会立即应用规则。可按字段更新；传入 `rules` 数组时替换完整规则列表。需要生效的规则应设置 `rules[].enabled` 为 `true`。

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
      "description": "allow admin and web",
      "enabled": true
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

`POST /api/v1/api-keys` 和 `PATCH /api/v1/api-keys/{id}` 使用相同的字段结构。创建时 `name` 必填。更新时，如果需要保留 `ip_whitelist`、`expires_at`、`disabled` 和 `container_uuids`，需把原值一并提交。省略限制字段会清空限制，省略 `disabled` 会重新启用密钥。省略或留空的 `name` 和 `scopes` 保留原值。

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
| `scopes` | 可选；权限范围。创建时省略会使用默认只读范围；传 `["*"]` 表示全部权限。 |
| `expires_at` | 可选；过期时间，空值表示不过期。 |
| `disabled` | 是否禁用该 Key。 |
| `container_uuids` | 可选；限制该 Key 只能访问指定容器。 |

明文 `data.key` 仅在创建成功时返回。API Key 管理权限作用于整个平台。持有 `apikey:create` 或 `apikey:update` 的 Key 可以设置任意权限（包括 `*`）和容器范围。仅向受信任的管理员授予这些权限。

## 面板访问来源策略

`GET /api/v1/access-policy` 读取面板访问白名单，`PUT /api/v1/access-policy` 更新策略。两者均需要 `admin:access` 权限。策略覆盖面板页面、登录入口和 API 请求，包括公开接口和票据下载。

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
    "host_port": 30080,
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

## 查询异步任务

提交磁盘、备份、恢复、克隆或实例模板任务后，HTTP `202` 的 `data.task_id` 用于查询 `GET /api/v1/tasks/{task_id}`。批量创建和批量操作的 `data` 是任务 ID 数组，按每个 ID 分别查询。单个实例创建 `POST /api/v1/containers` 在创建完成时返回 `201`。

```bash
curl --fail-with-body -H "X-API-Key: $CLICD_API_KEY" \
  "$CLICD_URL/api/v1/tasks/task-42"
```

```json
{
  "success": true,
  "data": {
    "id": "task-42",
    "type": "backup",
    "status": "done",
    "result": { "artifact_id": "example-backup-id" }
  }
}
```

`data.status` 为 `pending`、`running`、`done` 或 `failed`。失败时查看 `error` 和 `logs`，完成后可按任务类型读取 `result`。过期清理、任务不存在或无权访问时，详情接口返回 `404`。删除排队任务会取消执行，删除已完成记录仅清除历史，运行中的任务返回 `409`。详见[任务与历史指标](./tasks)。

主机及实例历史接口返回采样点数组。`ts` 为 Unix 毫秒，`cpu` 和 `memory` 为百分比，`network_rx`、`network_tx`、`disk_read` 和 `disk_write` 为字节/秒。最多保留 7 天，停机期间可能有空档。

## 磁盘和备份工作流

先读实例磁盘清单的 `data.system`、`data.data_disks` 和 `data.can_edit`。停止实例后，用下面的 JSON 请求操作；`size_gb` 为目标容量（GiB）。

| 操作 | 请求体 |
| --- | --- |
| 添加 LXC 数据盘 | `{"name":"data","size_gb":8,"mount_path":"/data","storage_pool_id":"disk-root"}` |
| 添加 KVM 数据盘 | `{"name":"data","size_gb":8,"storage_pool_id":"disk-root"}` |
| 扩容系统盘或数据盘 | `{"size_gb":20}` |
| 断开或重新连接 | `{"attached":false}` 或 `{"attached":true}` |
| 永久删除数据盘 | `{"delete_data":true}` |

存储池可从 `GET /api/v1/storage` 的 `data.pools` 选择。可选 `storage_pool_id` 省略时自动选择合适的存储池。`PUT /api/v1/storage` 接收完整的 `{"pools":[...]}` 清单；应以最近的 GET 结果为基础更新配置。

创建备份和保存实例模板接收 `{"name":"nightly","storage_pool_id":"disk-root"}`。完成后从任务结果取得 `result.artifact_id`，备份和模板也可分别从清单查询。冷复制会暂时停止运行中的源实例并在复制后尝试恢复其运行状态。

```bash
# 下载归档
curl --fail-with-body -H "X-API-Key: $CLICD_API_KEY" \
  "$CLICD_URL/api/v1/backups/$ARTIFACT_ID" -o backup.clicd.tar.gz

# 导入完整备份到指定池
curl --fail-with-body -H "X-API-Key: $CLICD_API_KEY" \
  -F 'name=imported-backup' -F 'file=@backup.clicd.tar.gz' \
  "$CLICD_URL/api/v1/backups?storage_pool_id=disk-root"
```

下载响应为 `application/gzip`，可使用 `HEAD` 查看元数据、用 `Range` 分段下载。浏览器也可先调用 `POST /api/v1/backups/{artifact_id}/download-ticket`，然后向同一个面板请求响应中的 `data.url`。v1 返回的相对地址为 `/api/v1/backup-download/{ticket}`。可直接凭票据 GET、HEAD 或分段下载，无需额外发送 API Key。票据有效期为 2 分钟，受面板来源策略约束。签发响应的 `data.expires_at` 是 RFC 3339 时间戳。

导入成功返回 HTTP `201` 和包含 `id` 的备份元数据。之后恢复备份、克隆或从模板创建时，发送 `{"name":"restored-01","storage_pool_id":"disk-root","assign_nat":true,"port_mapping_count":2}`。新实例默认停止，`assign_nat` 默认启用，端口配额默认 2（范围 1–64）。每次模板实例化请求创建一个实例，批量使用时需逐个提交不同名称。归档限制及系统要求见[磁盘、备份与克隆](./storage-backups)。

## 本地镜像上传和 KVM 安装介质

上传使用 `multipart/form-data`，必填字段为 `file` 与 `name`，单文件最多 32 GiB。可选字段有 `description`、`arch`、`sha256`、`provisioner`、`distro`、`release`，`storage_pool_id` 通过查询参数传递。

```bash
curl --fail-with-body -H "X-API-Key: $CLICD_API_KEY" \
  -F 'name=Debian installer' -F 'provisioner=manual-iso' \
  -F 'file=@debian.iso' \
  "$CLICD_URL/api/v1/images/upload?storage_pool_id=disk-root"
```

验证并启用成功后返回 HTTP `201`，`data` 包含 `id`、`name`、`format`、`size_bytes` 和 `sha256`。新建 KVM 时可将 `id` 用作 `template_id`。已有 KVM 可查询 `GET /api/v1/containers/{id}/installation-media` 的 `data.available`，停止实例后通过 PUT 发送 `{"image_id":"<iso_id>","boot":"cdrom"}`。安装结束可用 `{"image_id":"","boot":"disk"}` 弹出 ISO 并从磁盘启动。文件格式和安装方式见[镜像管理](./images)。

## IPv4 和 IPv6 NAT

已有实例的 `PUT .../public-ipv4` 和 `PUT .../ipv6-addresses` 接收 `{"mode":"auto","count":1}`、`{"mode":"custom","addresses":["<地址>"]}` 或 `{"mode":"clear"}`。先用 `GET /api/v1/routing` 查询宿主机地址池。

端口映射协议仅允许 `tcp` 或 `udp`。通过 GET 获取映射后，使用数组的零基序号编辑或删除，提交前应重新读取序号。IPv4 NAT 无独立公网地址时，宿主机端口必须落在路由配置中的 NAT 端口范围内。

`GET /api/v1/containers/{id}/ipv6-port-mappings` 返回 `enabled`、`sync_ipv4`、`host_ipv6`、`guest_ipv6`、`limit` 和 `mappings`。启用同步时映射由 IPv4 列表推导。先用 `PUT /api/v1/containers/{id}/ipv6-nat-sync` 提交 `{"enabled":false}`，再管理独立 IPv6 映射。POST/PUT 的映射字段与 IPv4 一致，`host_ip` 可省略以使用该实例的 IPv6 NAT 入口。LXC 的 IPv6 NAT 需要宿主机先启用 `/api/v1/ipv6/nat`，配置方式见[网络与路由](./networking)。

## 接口清单

最后一列为 API Key 权限。绑定容器 UUID 的 Key 同时受资源范围限制。修改全局面板语言时需使用未绑定容器的 Key。凭据缺失或无效通常返回 `401`，权限不足返回 `403`，资源或状态冲突返回 `409`。`/api/v1/containers/list` 为兼容别名，新客户端使用 `GET /api/v1/containers`。

### 总览

| 方法 | 路径 | 说明 | API Key 权限 |
| --- | --- | --- | --- |
| GET | `/api/v1/dashboard` | 控制面板统计 | `dashboard:read` |
| GET | `/api/v1/version` | 读取面板版本 | 公开 |
| GET | `/api/v1/host-info` | 主机资源 | `host:read` |
| GET | `/api/v1/host-report` | 主机巡检报告 | `host:read` |
| GET | `/api/v1/routing` | NAT/IPv4/IPv6 路由 | `routing:read` 或 `routing:write` |
| PUT | `/api/v1/routing` | 更新公网 IPv4/IPv6 池 | `routing:write` |
| POST | `/api/v1/routing/ipv4-scan` | 扫描公网 IPv4 段 | `routing:write` |
| GET | `/api/v1/ipv6/status` | IPv6 状态 | `ipv6:read` |
| GET | `/api/v1/ipv6/nat` | 宿主机 LXC IPv6 NAT 配置及状态 | `admin:access` 及 `ipv6:read` |
| PUT | `/api/v1/ipv6/nat` | 启用、禁用或配置 LXC IPv6 NAT | `admin:access` 及 `routing:write` |
| GET | `/api/v1/tasks` | 任务队列 | `task:read` |
| GET | `/api/v1/tasks/{task_id}` | 读取单个任务及其阶段、结果和错误 | `task:read` |
| DELETE | `/api/v1/tasks/{task_id}` | 删除任务 | `task:delete` |
| GET | `/api/v1/task-queue/settings` | 读取并发上限、运行中和排队任务数 | `admin:access` |
| PUT/POST | `/api/v1/task-queue/settings` | 设置任务并发上限 | `admin:access` |

### 容器

| 方法 | 路径 | 说明 | API Key 权限 |
| --- | --- | --- | --- |
| GET | `/api/v1/containers` | 容器列表（推荐） | `container:read` |
| GET | `/api/v1/containers/list` | 容器列表兼容 GET 写法 | `container:read` |
| POST | `/api/v1/containers/list` | 容器列表兼容 POST 写法 | `container:read` |
| POST | `/api/v1/containers` | 创建容器 | `container:create` |
| GET | `/api/v1/containers/{id\|uuid\|name}` | 容器详情 | `container:read` |
| POST | `/api/v1/containers/{id}/start` | 开机 | `container:power` |
| POST | `/api/v1/containers/{id}/stop` | 关机 | `container:power` |
| POST | `/api/v1/containers/{id}/restart` | 重启 | `container:power` |
| POST | `/api/v1/containers/{id}/reinstall` | 重装 | `container:reinstall` |
| DELETE | `/api/v1/containers/{id}/delete` | 删除 | `container:delete` |
| GET | `/api/v1/containers/{id}/usage` | 资源用量 | `container:read` |
| GET | `/api/v1/containers/{id}/history` | 持久化资源历史，最多 7 天 | `container:read` |
| GET | `/api/v1/containers/{id}/installation-media` | KVM 安装介质、启动方式及可选 ISO | `container:read` |
| PUT | `/api/v1/containers/{id}/installation-media` | 停止状态下挂载/更换/弹出 ISO 与设置启动方式 | `container:reinstall` |
| GET | `/api/v1/containers/{id}/traffic` | 流量统计 | `container:read` |
| POST | `/api/v1/containers/{id}/traffic-reset` | 重置流量 | `container:traffic` |
| PUT | `/api/v1/containers/{id}/traffic-limit` | 调整流量限制 | `container:traffic` |
| PUT | `/api/v1/containers/{id}/resource-limit` | 调整资源限制 | `container:resize` |
| PUT | `/api/v1/containers/{id}/expiry` | 调整到期时间 | `container:resize` |
| POST | `/api/v1/containers/{id}/reset-password` | 重置 SSH 密码 | `container:password` |
| POST | `/api/v1/containers/{id}/ipv6` | 分配 IPv6 | `ipv6:assign` |
| PUT | `/api/v1/containers/{id}/public-ipv4` | 更新公网 IPv4 分配 | `container:network` |
| PUT | `/api/v1/containers/{id}/ipv6-addresses` | 更新 IPv6 分配 | `ipv6:assign` |

### 端口与快照

| 方法 | 路径 | 说明 | API Key 权限 |
| --- | --- | --- | --- |
| GET | `/api/v1/containers/{id}/random-port` | 随机可用端口；可传 `host_ip` 查询指定宿主机 IP | `container:network` |
| GET | `/api/v1/containers/{id}/port-mappings` | IPv4 NAT 端口映射清单 | `container:read` |
| POST | `/api/v1/containers/{id}/port-mappings` | 添加端口映射 | `container:network` |
| PUT | `/api/v1/containers/{id}/port-mappings/{index}` | 更新端口映射 | `container:network` |
| DELETE | `/api/v1/containers/{id}/port-mappings/{index}` | 删除端口映射 | `container:network` |
| GET | `/api/v1/containers/{id}/ipv6-port-mappings` | IPv6 NAT 状态与端口映射清单 | `container:network` |
| POST | `/api/v1/containers/{id}/ipv6-port-mappings` | 添加独立 IPv6 端口映射 | `container:network` |
| PUT | `/api/v1/containers/{id}/ipv6-port-mappings/{index}` | 修改独立 IPv6 端口映射 | `container:network` |
| DELETE | `/api/v1/containers/{id}/ipv6-port-mappings/{index}` | 删除独立 IPv6 端口映射 | `container:network` |
| PUT | `/api/v1/containers/{id}/ipv6-nat-sync` | 设置是否镜像 IPv4 映射 | `container:network` |
| GET | `/api/v1/containers/{id}/firewall` | 获取容器防火墙设置 | `container:network` |
| PUT | `/api/v1/containers/{id}/firewall` | 更新容器防火墙设置 | `container:network` |
| GET | `/api/v1/snapshots` | 快照总览 | `snapshot:read` |
| GET | `/api/v1/containers/{id}/snapshots` | 容器快照 | `snapshot:read` |
| POST | `/api/v1/containers/{id}/snapshots` | 创建快照 | `snapshot:create` |
| DELETE | `/api/v1/containers/{id}/snapshots/{snapshot_id}` | 删除快照 | `snapshot:delete` |
| POST | `/api/v1/containers/{id}/snapshots/{snapshot_id}/restore` | 恢复快照 | `snapshot:restore` |
| POST | `/api/v1/containers/{id}/snapshots/schedule` | 计划快照 | `snapshot:schedule` |
| PUT | `/api/v1/containers/{id}/snapshots/quota` | 快照配额 | `snapshot:schedule` |

### 磁盘、完整备份与实例模板

磁盘变更、完整备份、恢复、克隆和模板操作仅接受管理员会话或具备表列权限且未绑定容器的 API Key。子用户可读取自己实例的磁盘清单。运行与存储要求见[磁盘、备份与克隆](./storage-backups)。

| 方法 | 路径 | 说明 | API Key 权限 |
| --- | --- | --- | --- |
| GET | `/api/v1/containers/{id}/disks` | 真实容量、已分配空间与编辑能力 | `container:read` |
| POST | `/api/v1/containers/{id}/disks` | 添加受管数据盘 | `container:resize` |
| PUT | `/api/v1/containers/{id}/disks/system` | 离线扩容系统盘，只增不减 | `container:resize` |
| PUT | `/api/v1/containers/{id}/disks/{disk_id}` | 离线扩容数据盘 | `container:resize` |
| PUT | `/api/v1/containers/{id}/disks/{disk_id}/attachment` | 连接/断开数据盘 | `container:resize` |
| DELETE | `/api/v1/containers/{id}/disks/{disk_id}` | 永久删除数据盘，需 `delete_data: true` | `container:resize` |
| POST | `/api/v1/containers/{id}/backup` | 创建完整备份 | `admin:access` |
| POST | `/api/v1/containers/{id}/clone` | 完整克隆为新停止实例 | `admin:access` |
| POST | `/api/v1/containers/{id}/template` | 保存已安装实例模板 | `admin:access` |
| GET | `/api/v1/backups` | 完整备份列表 | `admin:access` |
| POST | `/api/v1/backups` | 上传 `.clicd.tar.gz` 完整备份 | `admin:access` |
| GET/HEAD | `/api/v1/backups/{artifact_id}` | 下载完整备份 | `admin:access` |
| POST | `/api/v1/backups/{artifact_id}/download-ticket` | 签发临时备份下载地址 | `admin:access` |
| GET/HEAD | `/api/v1/backup-download/{ticket}` | 凭 2 分钟有效的票据下载单个备份 | 下载票据 |
| DELETE | `/api/v1/backups/{artifact_id}` | 删除备份文件 | `admin:access` |
| POST | `/api/v1/backups/{artifact_id}/restore` | 恢复为新停止实例 | `admin:access` |
| GET | `/api/v1/instance-templates` | 已安装实例模板列表 | `admin:access` |
| DELETE | `/api/v1/instance-templates/{artifact_id}` | 删除实例模板 | `admin:access` |
| POST | `/api/v1/instance-templates/{artifact_id}/instantiate` | 从模板创建新停止实例 | `admin:access` |

请求示例见[磁盘和备份工作流](#磁盘和备份工作流)。

### 平台管理

| 方法 | 路径 | 说明 | API Key 权限 |
| --- | --- | --- | --- |
| GET | `/api/v1/templates` | 模板列表 | `image:read` |
| GET | `/api/v1/images` | 镜像管理列表 | `image:read` |
| GET | `/api/v1/images/enabled` | 已启用且已下载的镜像；支持 `type=lxc\|kvm` | `image:read` |
| POST | `/api/v1/images/custom` | 添加第三方 LXC/KVM 镜像源 | `admin:access` 及 `image:download` |
| DELETE | `/api/v1/images/custom` | 移除第三方 LXC/KVM 镜像源及缓存 | `admin:access` 及 `image:delete` |
| POST | `/api/v1/images/download` | 下载镜像 | `admin:access` 及 `image:download` |
| POST | `/api/v1/images/cancel` | 取消镜像下载 | `admin:access` 及 `image:download` |
| DELETE | `/api/v1/images/delete` | 删除镜像缓存 | `admin:access` 及 `image:delete` |
| PUT | `/api/v1/images/toggle` | 启用/禁用镜像 | `admin:access` 及 `image:toggle` |
| POST | `/api/v1/images/upload` | 本地 ISO/QCOW2 上传，`multipart/form-data`，文件最多 32 GiB | `admin:access` 及 `image:download` 及 `image:toggle` |
| GET | `/api/v1/storage` | 宿主机存储池和磁盘清单 | `admin:access` |
| PUT | `/api/v1/storage` | 替换受管存储池配置 | `admin:access` |
| GET | `/api/v1/host-history` | 持久化主机历史指标，最多 7 天 | `host:read` |
| GET | `/api/v1/swap` | Swap 信息 | `swap:read` |
| POST | `/api/v1/swap` | 调整 Swap | `swap:manage` |
| GET | `/api/v1/language` | 当前面板语言 | 公开 |
| POST/PUT | `/api/v1/language` | 更新面板语言 | `admin:access` |
| GET | `/api/v1/ssl` | SSL 设置 | `admin:access` |
| PUT | `/api/v1/ssl` | 更新 SSL 设置 | `admin:access` |
| GET | `/api/v1/webssh-origins` | WebSSH Origin 白名单 | `admin:access` |
| PUT | `/api/v1/webssh-origins` | 更新 WebSSH Origin 白名单 | `admin:access` |
| GET | `/api/v1/access-policy` | 读取面板来源策略 | `admin:access` |
| PUT | `/api/v1/access-policy` | 更新面板来源策略 | `admin:access` |
| POST | `/api/v1/batch-create` | 批量创建容器 | `container:create` |
| POST | `/api/v1/batch-action` | 批量开关机/删除/重装 | 按操作授予 `container:power`、`container:delete` 或 `container:reinstall` |
| POST | `/api/v1/ssh-ticket` | 创建 WebSSH 票据 | `terminal:ssh` |
| POST | `/api/v1/vnc-ticket` | 创建 WebVNC 票据 | `terminal:vnc` |

更新语言可发送 `{"language":"en"}` 或 `{"language":"zh"}`。`/api/v1/language` 和 `/api/language` 的写入均需管理员认证。任务并发数设置请求体为 `{"concurrency":2}`，范围 1–16。

### 账号与日志

| 方法 | 路径 | 说明 | API Key 权限 |
| --- | --- | --- | --- |
| POST | `/api/v1/sub-user/create` | 创建子用户链接 | `subuser:create` |
| GET | `/api/v1/sub-users` | 子用户列表 | `subuser:read` |
| POST | `/api/v1/sub-users/{id}/rotate-password` | 轮换子用户密码 | `subuser:update` |
| PUT | `/api/v1/sub-users/{id}/images` | 设置子用户镜像权限 | `subuser:update` |
| GET | `/api/v1/sub-users/{id}/audit-logs` | 子用户操作日志 | `audit:read` |
| GET | `/api/v1/sub-users/{id}/login-logs` | 子用户登录日志 | `loginlog:read` |
| GET | `/api/v1/audit-logs` | 操作日志 | `audit:read` |
| GET | `/api/v1/login-logs` | 登录日志 | `loginlog:read` |
| GET | `/api/v1/api-keys` | API Key 列表 | `apikey:read` |
| POST | `/api/v1/api-keys` | 创建 API Key | `apikey:create` |
| PATCH | `/api/v1/api-keys/{id}` | 更新 API Key | `apikey:update` |
| DELETE | `/api/v1/api-keys/{id}` | 删除 API Key | `apikey:delete` |

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
        "lxc_privileged": true,
        "lxc_nesting": true,
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
      "virtualization": "lxc",
      "lxc_privileged": true,
      "lxc_nesting": true,
      "status": "running",
      "ip": "10.0.0.10",
      "ipv6": "2001:db8:100::1005",
      "ssh_port": 22004,
      "ssh_password": "***"
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
