# 容器管理

容器管理是 CLICD 的核心模块，覆盖创建、生命周期控制、资源限制、网络映射、流量统计、密码重置和控制台访问。

## 容器列表

列表页用于扫描所有容器状态。管理员可以查看全部容器，子用户只能看到授权范围内的容器。

常见字段包括：

- ID、UUID、名称。
- 虚拟化类型。
- 运行状态。
- IP、IPv6。
- CPU、内存、磁盘限制。
- 流量使用量和流量上限。
- 到期时间。

## 创建容器

创建时需要选择模板，并设置资源配额。批量创建可以通过面板或 API 完成，适合一次性发放多个容器。

```http
POST /api/v1/containers
POST /api/v1/batch-create
```

Linux 容器和 Linux KVM 虚拟机创建时支持配置 SSH 登录方式：

- `auto_password`：自动生成 root SSH 密码。
- `password`：使用自定义 `ssh_password`。
- `key`：写入一行 `ssh_public_key`，仍会保留可用于 WebSSH 的密码。

网络分配可以按需组合 NAT、公网 IPv4 和 IPv6。API 字段保持为 `assign_nat`、`assign_ipv4`、`public_ipv4s`、`assign_ipv6`、`ipv6_addresses` 等可选字段，未传时沿用默认行为。

LXC 创建还支持两个隔离选项：`nesting=true` 允许 Docker/LXC-in-LXC 和 systemd 所需的嵌套能力；`privileged=true` 创建特权容器，仅管理员可用，适合可信工作负载。特权模式会降低隔离强度，已有实例不能在线切换模式，默认应保持 `privileged=false`。

实例名称可以单独修改。该操作只更新面板展示名称，不改变运行时名称、UUID、磁盘路径、MAC 地址或系统 hostname：

```http
PATCH /api/v1/containers/{id}/name
```

请求体为 `{ "name": "new-name" }`，需要 `container:resize` 权限。

## 生命周期操作

```http
POST /api/v1/containers/{id}/start
POST /api/v1/containers/{id}/stop
POST /api/v1/containers/{id}/restart
POST /api/v1/containers/{id}/reinstall
DELETE /api/v1/containers/{id}/delete
```

开关机、重装、删除等操作会进入任务队列。调用后可通过 `GET /api/v1/tasks` 查看执行状态。

重装 Linux 系统时可传 `ssh_auth_mode`、`ssh_password`、`ssh_public_key`。`ssh_auth_mode=keep` 表示沿用当前 SSH 密码；不传这些字段时保持旧行为。

## 资源与流量

容器详情页支持查看资源用量，调整流量限制、资源限制和到期时间。

系统盘扩容和数据盘管理从详情页的磁盘管理入口操作，要求实例停止。完整备份、恢复、克隆及保存实例模板见[磁盘、备份与克隆](./storage-backups)。运行进度与错误可在[任务页面](./tasks)查看。

```http
GET /api/v1/containers/{id}/usage
GET /api/v1/containers/{id}/traffic
POST /api/v1/containers/{id}/traffic-reset
PUT /api/v1/containers/{id}/traffic-limit
PUT /api/v1/containers/{id}/resource-limit
PUT /api/v1/containers/{id}/expiry
```

## NAT 端口管理

容器详情页的 NAT 端口管理支持新增、编辑和删除映射。新增和编辑会在弹窗里完成，便于集中填写名称、协议、外部端口和内部端口。

```http
GET /api/v1/containers/{id}/random-port
POST /api/v1/containers/{id}/port-mappings
PUT /api/v1/containers/{id}/port-mappings/{index}
DELETE /api/v1/containers/{id}/port-mappings/{index}
```

子用户模式下，管理员可限制子用户只能调整内部端口，避免修改宿主机对外端口和协议。

## 远程控制台

```http
POST /api/v1/ssh-ticket
POST /api/v1/vnc-ticket
```

票据只适合短时间使用，返回后应立即用于 WebSSH 或 WebVNC 连接，不要持久化保存。
