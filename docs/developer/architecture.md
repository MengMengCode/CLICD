# 系统架构

CLICD 由 Go 后端、React 前端和宿主机虚拟化能力组成。

## 后端

后端入口在 `backend/main.go`，HTTP 服务路由集中在 `backend/internal/server/server.go`。主要模块：

- `internal/api`：Web 面板和 `/api/v1` 的 HTTP 接口。
- `internal/config`：配置和 SQLite 存储。
- `internal/lxc`：LXC 容器管理。
- `internal/kvm`：KVM/libvirt 虚拟机管理。
- `internal/transfer`：全盘归档、校验、可移植备份和实例模板。
- `internal/cli`：命令行管理入口。
- `internal/server`：静态前端嵌入和 HTTP 服务。
- `internal/version`：版本号。

## 前端

前端入口在 `frontend/src/main.tsx`，页面位于 `frontend/src/pages`，通用组件位于 `frontend/src/components`。

主要页面：

- 控制面板：`Dashboard.tsx`
- 容器列表：`Containers.tsx`
- 容器详情：`ContainerDetail.tsx`
- 镜像管理：`ImageManagement.tsx`
- 快照管理：`Snapshots.tsx`
- 任务历史：`Tasks.tsx`
- 完整备份与实例模板：`PortableLibrary.tsx`（`/backups`、`/instance-templates`）
- 路由管理：`Routing.tsx`
- API 集成：`ApiIntegration.tsx`
- 主机报告：`HostReport.tsx`
- 子用户管理：`SubUserManagement.tsx`

## 前端嵌入

生产构建时，前端产物会放入 `backend/internal/server/web`，后端通过 Go embed 提供静态文件，并对非 API 路由返回 SPA 入口。

## 接口分层

- `/api/*`：Web 面板和兼容接口。
- `/api/v1/*`：推荐给外部自动化系统使用的版本化接口。
- WebSSH 和 WebVNC 使用短期票据后建立 WebSocket 连接。

## 任务与存储状态

磁盘、备份、恢复、克隆和模板任务复用同实例队列锁。SQLite 独立历史表保存任务事件/结果及约 30 秒粒度的指标；通用运行中任务遇服务重启会记录为中断失败，待执行任务恢复排队。容器表同时持久化数据盘清单、独立系统盘模式和安装介质状态。

KVM 数据盘以受管 QCOW2 文件写入域 XML。LXC 数据盘使用 rootfs 外的 ext4 镜像挂载点和受管 `lxc.mount.entry`，避免冷复制 rootfs 时混入数据盘。完整归档逐盘校验并独立复制；旧快照在存在数据盘时拒绝创建和恢复。详见[存储边界](../features/storage-backups)和[任务历史](../features/tasks)。
