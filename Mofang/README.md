# CLICD 魔方财务对接模块

这是用于智简魔方 / IDCSMART 的 CLICD 服务器模块。模块通过 CLICD API 完成实例开通、删除、开关机、重启、重装、改密、资源变更、流量重置、NAT 端口映射管理、实例信息展示和 WebSSH 入口。

1.0.12 新增 IPv6 NAT 客户区：节点启用 NAT66 后可查看 ULA 端点、切换 IPv4 同步和管理独立 IPv6 映射；未升级节点会提示接口不可用。操作目标始终由当前魔方产品对应的实例确定。

## 文件结构

```text
clicd.php
README.md
handlers/
  webssh.php
templates/
  firewall.html
  info.html
  nat.html
  nat6.html
```

安装时请保持目录结构不变，将整个 `clicd` 目录放入魔方服务器模块目录：

```text
public/plugins/servers/clicd/
```

## 服务器配置

在魔方后台添加服务器时，模块名称选择 `clicd`。

CLICD 面板地址建议使用 HTTPS：

```text
主机名 = https://0.0.0.0:8999
```

也可以拆分填写：

```text
IP地址 = 0.0.0.0
端口      = 8999
secure    = 开启
```

API Key 可以填写在以下任意一个字段中：

```text
Hash
密码
```

模块请求 CLICD 时会同时携带：

```text
X-API-Key: clicd_sk_xxxx
Authorization: Bearer clicd_sk_xxxx
Content-Type: application/json
```

## 产品配置项

| 字段 | 说明 |
| --- | --- |
| `virtualization` | 虚拟化类型，`lxc` 或 `kvm` |
| `template_id` | CLICD 模板 / 镜像 ID |
| `vcpu` | CPU 核心数 |
| `cpu_percent` | CPU 使用率限制，`0` 表示不额外限制 |
| `ram_mb` | 内存，单位 MB |
| `disk_gb` | 系统盘，单位 GB |
| `network_bw_mbps` | 带宽，单位 Mbps |
| `traffic_mode` | `total` 总流量，或 `in_out` 入 / 出分开 |
| `monthly_traffic_gb` | 月流量 GB |
| `traffic_in_gb` | 入站流量 GB，`in_out` 模式使用 |
| `traffic_out_gb` | 出站流量 GB，`in_out` 模式使用 |
| `io_speed_mbps` | 磁盘 IO 限制，`0` 表示不限制 |
| `port_mapping_count` | 开通时分配的 NAT 端口数量，最小 2 |
| `ipv6_nat_sync_ipv4` | IPv6 NAT 是否同步 IPv4 映射，默认启用；关闭后独立管理 |
| `ipv6_port_mapping_count` | IPv6 NAT 端口配额，`0` 继承 IPv4，最大 `64`；不自动开启 IPv4 NAT |
| `snapshot_limit` | 快照配额 |
| `extra_ports` | 额外映射的容器端口，逗号分隔，例如 `80,443` |
| `assign_ipv6` | 开通时是否自动分配 IPv6 |
| `sync_expiry` | 是否同步魔方到期时间到 CLICD |

客户产品的 `domain` 会作为 CLICD 容器名称。模块会自动把不适合作为容器名的字符替换为 `-`。

## 开通后字段同步

开通、同步、重装、改密后，模块会从 CLICD 容器详情拉取最新信息并写回魔方主机表：

| 魔方字段 | 写入内容 |
| --- | --- |
| `dedicatedip` | NAT 外网 IP，优先使用 API 返回的公网字段，否则使用服务器 IP |
| `username` | 固定写入 `root` |
| `password` | CLICD 返回的 SSH 密码，兼容魔方 `cmf_encrypt()` |
| `port` | CLICD 返回的 `ssh_port` |
| `domainstatus` | CLICD 状态为 `running` 时写 `Active`，否则写 `Suspended` |

如果接口返回的密码是 `***` 这类脱敏值，模块不会覆盖魔方里已有密码。

## 客户区页面

模块提供四个客户区选项卡：

```text
实例信息
NAT转发
IPv6 NAT
防火墙
```

客户区按钮提供：

```text
WebSSH
```

## 实例信息

实例信息页展示：

- 实例名称、运行状态、SSH 地址、IPv6
- CPU、内存、负载、磁盘圆环状态
- 月流量进度
- CPU 使用率、内存使用、网络流量、磁盘 IO 图表
- IPv4、SSH 端口、SSH 密码、资源配置、到期时间

图表数据通过客户区懒加载接口获取，不会强制刷新整个魔方页面。页面首次打开会加载一次数据，之后由用户选择是否自动刷新：

```text
不刷新
10 秒
1 分钟
5 分钟
10 分钟
```

也可以点击“立即刷新”手动刷新一次。当前 CLICD 用量接口返回的是实时值，不是历史数组；图表曲线由客户区前端持续采样生成。若需要打开页面立即显示历史曲线，需要 CLICD 额外提供历史指标接口。

流量显示支持智能单位，小流量会显示 B / KB / MB，大流量显示 GB，例如：

```text
370.5 KB / 100 GB
```

模块会优先调用：

```text
GET /api/v1/containers/{name}/usage
GET /api/v1/containers/{name}/traffic
```

如果 `/api/v1/containers/{name}/usage` 不可用，模块会在容器详情存在 `uuid` 时尝试兼容：

```text
GET /api/containers/{uuid}/usage
```

已兼容的常见用量字段包括：

```text
cpu_usage_pct
memory_usage_bytes
disk_usage_bytes
network_rx_bps
network_tx_bps
disk_read_bps
disk_write_bps
rx_used_bytes
tx_used_bytes
total_used_bytes
limit_gb
used_pct
```

## NAT 转发

NAT 转发是独立页面，支持：

- 查看端口映射
- 获取随机可用端口
- 添加端口映射
- 修改端口映射
- 删除端口映射

删除端口映射时使用页面内确认弹窗，不使用浏览器自带确认框。

使用的 CLICD API：

```text
GET    /api/v1/containers/{id|uuid|name}
GET    /api/v1/containers/{id}/random-port
POST   /api/v1/containers/{id}/port-mappings
PUT    /api/v1/containers/{id}/port-mappings/{index}
DELETE /api/v1/containers/{id}/port-mappings/{index}
```

添加 / 修改 NAT 映射时必须使用 JSON 请求体，例如：

```json
{
  "container_port": 8080,
  "host_port": 61320,
  "protocol": "tcp",
  "description": "HTTP"
}
```

## 防火墙

防火墙是独立客户区页面，支持：

- 查看防火墙启用状态、默认动作和规则列表
- 启用 / 停用防火墙
- 设置默认动作：未匹配拒绝或未匹配放行
- 添加规则
- 编辑规则
- 删除规则
- 单独启用 / 停用某条规则

页面会先在前端修改规则列表和开关状态，点击“保存设置”后才统一同步到 CLICD。这样可以避免每次切换开关、修改默认动作或编辑规则时都立即请求后端，减少客户区卡顿。

注意：防火墙关闭时也可以保存规则；关闭只表示暂时不接管该容器流量，不代表规则必须清空。

使用的 CLICD API：

```text
GET /api/v1/containers/{id}/firewall
PUT /api/v1/containers/{id}/firewall
```

更新防火墙时必须使用 JSON 请求体，例如：

```json
{
  "enabled": true,
  "default_action": "ACCEPT",
  "rules": [
    {
      "id": "",
      "network": "ipv4",
      "direction": "in",
      "protocol": "tcp",
      "port": "22",
      "source_ip": "",
      "action": "ACCEPT",
      "description": "Allow SSH",
      "enabled": true
    }
  ]
}
```

规则字段说明：

| 字段 | 说明 |
| --- | --- |
| `network` | 网络范围，常用 `ipv4`，也支持 `ipv6` / `all` |
| `direction` | 方向，`in` 入站，`out` 出站 |
| `protocol` | 协议，`tcp` 或 `udp` |
| `port` | 端口，可填写单端口、逗号分隔端口或端口段，例如 `22`、`80,443`、`8000-9000` |
| `source_ip` | 来源 IP / CIDR，留空表示任意来源 |
| `action` | 动作，`ACCEPT` 放行，`DROP` 拒绝 |
| `description` | 规则描述 |
| `enabled` | 是否启用该规则 |

IPv4 NAT 入站规则的端口按容器内部端口匹配，不是宿主机公网端口。例如公网 `22023 -> 容器 22`，防火墙规则端口应填写 `22`。
## WebSSH

WebSSH 按钮会调用：

```text
POST /api/v1/ssh-ticket
```

请求体：

```json
{
  "container_name": "example-vm"
}
```

接口返回 60 秒有效票据后，模块会打开本地 handler：

```text
/plugins/servers/clicd/handlers/webssh.php
```

浏览器会从该页面直连 CLICD：

```text
wss://0.0.0.0:8999/api/ssh?container=example-vm
Sec-WebSocket-Protocol: clicd-ticket.xxxxx
```

注意：WebSSH 受浏览器安全策略和 CLICD 后端 Origin 校验影响。魔方客户区通常是 HTTPS，因此 CLICD 面板也必须启用 HTTPS/WSS。请把魔方服务器配置里的 `主机名` 改为 `https://0.0.0.0:8999`，或把 `secure` 设为 `开启`。

新版 CLICD 已支持 WebSSH Origin 放行。部署时需要在 CLICD 后端把魔方财务客户区域名加入 WebSSH Origin 白名单，例如：

```text
https://www.example.com
```

如果 WebSSH 页面显示 `WebSocket error`、`Disconnected code=1006`，但直接以 CLICD 自身 Origin 测试能返回 `101 Switching Protocols`，通常说明 CLICD 后端未放行魔方客户区域名的 WebSocket Origin。此时请检查 CLICD 的 WebSSH Origin 白名单配置；前端页面无法伪造浏览器 Origin。

## 支持的魔方操作

| 魔方操作 | CLICD API |
| --- | --- |
| 连接测试 | `GET /api/v1/dashboard` |
| 开通 | `POST /api/v1/containers` |
| 删除 | `DELETE /api/v1/containers/{name}/delete` |
| 开机 | `POST /api/v1/containers/{name}/start` |
| 关机 | `POST /api/v1/containers/{name}/stop` |
| 重启 | `POST /api/v1/containers/{name}/restart` |
| 重装 | `POST /api/v1/containers/{name}/reinstall` |
| 改密 | `POST /api/v1/containers/{name}/reset-password` |
| 重置流量 | `POST /api/v1/containers/{name}/traffic-reset` |
| 变更资源 | `PUT /api/v1/containers/{name}/resource-limit` |
| 变更流量 | `PUT /api/v1/containers/{name}/traffic-limit` |
| 同步到期 | `PUT /api/v1/containers/{name}/expiry` |
| 查询防火墙 | `GET /api/v1/containers/{id}/firewall` |
| 更新防火墙 | `PUT /api/v1/containers/{id}/firewall` |
| WebSSH | `POST /api/v1/ssh-ticket` |

## 建议 API 权限

API Key 至少需要以下权限，具体名称以 CLICD 后端实际权限系统为准：

```text
dashboard:read
container:read
container:create
container:power
container:delete
container:reinstall
container:password
container:traffic
container:resize
container:port
container:firewall
task:read
ssh-ticket:create
```

如果 API Key 使用 `*` 或 `admin:*`，通常可以覆盖上述权限。

## 建议先测试的 curl

连接测试：

```bash
curl -H "X-API-Key: clicd_sk_xxxx" \
  https://0.0.0.0:8999/api/v1/dashboard
```

容器详情：

```bash
curl -H "X-API-Key: clicd_sk_xxxx" \
  https://0.0.0.0:8999/api/v1/containers/example-vm
```

资源用量：

```bash
curl -H "X-API-Key: clicd_sk_xxxx" \
  https://0.0.0.0:8999/api/v1/containers/example-vm/usage
```

流量统计：

```bash
curl -H "X-API-Key: clicd_sk_xxxx" \
  https://0.0.0.0:8999/api/v1/containers/example-vm/traffic
```

修改 NAT：

```bash
curl --location --request PUT \
  "https://0.0.0.0:8999/api/v1/containers/10/port-mappings/1" \
  --header "X-API-Key: clicd_sk_xxxx" \
  --header "Authorization: Bearer clicd_sk_xxxx" \
  --header "Content-Type: application/json" \
  --data-raw '{"container_port":8081,"host_port":61320,"protocol":"tcp","description":"HTTP"}'
```

查询防火墙：

```bash
curl -H "X-API-Key: clicd_sk_xxxx" \
  https://0.0.0.0:8999/api/v1/containers/10/firewall
```

更新防火墙：

```bash
curl --location --request PUT \
  "https://0.0.0.0:8999/api/v1/containers/10/firewall" \
  --header "X-API-Key: clicd_sk_xxxx" \
  --header "Content-Type: application/json" \
  --data-raw '{"enabled":true,"default_action":"ACCEPT","rules":[{"id":"","network":"ipv4","direction":"in","protocol":"tcp","port":"22","source_ip":"","action":"ACCEPT","description":"Allow SSH","enabled":true}]}'
```
创建 WebSSH 票据：

```bash
curl --location --request POST \
  "https://0.0.0.0:8999/api/v1/ssh-ticket" \
  --header "X-API-Key: clicd_sk_xxxx" \
  --header "Content-Type: application/json" \
  --data-raw '{"container_name":"example-vm"}'
```

## 常见问题

### NAT 修改不生效

确认请求体必须是 JSON，不要使用 `multipart/form-data`。正确请求头：

```text
Content-Type: application/json
```

### 防火墙获取提示“不支持的方法”

请确认模块版本已经包含防火墙页签修复。客户区防火墙列表应通过模块公开的 `firewallList` 调用，再由模块向 CLICD 发起：

```text
GET /api/v1/containers/{id}/firewall
```

如果页面或二开代码直接把读取请求改成 `POST /api/v1/containers/{id}/firewall`，CLICD 会返回“不支持的方法”。

### 防火墙保存后规则为空

请确认更新接口最终发往 CLICD 的请求体是 JSON，并且包含 `rules` 数组。防火墙关闭时也可以保存规则，`enabled: false` 不应自动清空 `rules`。

正确请求体示例：

```json
{
  "enabled": false,
  "default_action": "ACCEPT",
  "rules": [
    {
      "id": "",
      "network": "ipv4",
      "direction": "in",
      "protocol": "tcp",
      "port": "22",
      "source_ip": "",
      "action": "ACCEPT",
      "description": "Allow SSH",
      "enabled": true
    }
  ]
}
```
### 图表刚打开只有一条横线

CLICD 当前用量接口返回的是实时值，不是历史序列。页面刚打开时只有一个采样点，所以会显示当前值横线。选择 `10 秒` 自动刷新或点击“立即刷新”多采样几次后，会逐步形成折线。

### 流量显示为 0

旧版本只显示 GB，小流量换算后会被四舍五入成 `0 GB`。当前版本已改为智能单位，会显示 B / KB / MB / GB。

### 防火墙

防火墙是独立客户区页面，支持：

- 查看防火墙启用状态、默认动作和规则列表
- 启用 / 停用防火墙
- 设置默认动作：未匹配拒绝或未匹配放行
- 添加规则
- 编辑规则
- 删除规则
- 单独启用 / 停用某条规则

页面会先在前端修改规则列表和开关状态，点击“保存设置”后才统一同步到 CLICD。这样可以避免每次切换开关、修改默认动作或编辑规则时都立即请求后端，减少客户区卡顿。

注意：防火墙关闭时也可以保存规则；关闭只表示暂时不接管该容器流量，不代表规则必须清空。

使用的 CLICD API：

```text
GET /api/v1/containers/{id}/firewall
PUT /api/v1/containers/{id}/firewall
```

更新防火墙时必须使用 JSON 请求体，例如：

```json
{
  "enabled": true,
  "default_action": "ACCEPT",
  "rules": [
    {
      "id": "",
      "network": "ipv4",
      "direction": "in",
      "protocol": "tcp",
      "port": "22",
      "source_ip": "",
      "action": "ACCEPT",
      "description": "Allow SSH",
      "enabled": true
    }
  ]
}
```

规则字段说明：

| 字段 | 说明 |
| --- | --- |
| `network` | 网络范围，常用 `ipv4`，也支持 `ipv6` / `all` |
| `direction` | 方向，`in` 入站，`out` 出站 |
| `protocol` | 协议，`tcp` 或 `udp` |
| `port` | 端口，可填写单端口、逗号分隔端口或端口段，例如 `22`、`80,443`、`8000-9000` |
| `source_ip` | 来源 IP / CIDR，留空表示任意来源 |
| `action` | 动作，`ACCEPT` 放行，`DROP` 拒绝 |
| `description` | 规则描述 |
| `enabled` | 是否启用该规则 |

IPv4 NAT 入站规则的端口按容器内部端口匹配，不是宿主机公网端口。例如公网 `22023 -> 容器 22`，防火墙规则端口应填写 `22`。
## WebSSH 打不开或提示不安全 WebSocket

请确认 CLICD 面板已经启用 HTTPS/WSS，并且魔方服务器配置使用 HTTPS：

```text
server_host = https://0.0.0.0:8999
```

如果仍然使用 `http://`，模块会生成 `ws://` 地址，HTTPS 客户区页面会被浏览器拦截。

如果 WSS 证书正常但仍返回 `Forbidden` 或浏览器显示 `code=1006`，请检查 CLICD 的 WebSSH Origin 白名单。新版 CLICD 已支持放行魔方财务域名，需要把魔方客户区访问域名完整加入白名单，例如：

```text
https://www.example.com
```

注意需要填写浏览器实际访问魔方客户区时的协议和域名，`http` / `https`、带不带 `www` 都要与实际访问地址一致。

### 开通后魔方里的 IP、端口、密码不对

执行“同步状态”或重装 / 改密后，模块会重新拉取容器详情。请确认 CLICD 容器详情接口能返回：

```text
ssh_port
ssh_password
status
```

公网 IP 优先使用 `nat_public_ip/public_ip/host_ip/external_ip/node_ip/nat_host` 等字段；如果接口没有返回，则使用魔方服务器配置的 IP。
