# 系统架构

CLICD 由 Go 后端、React 前端和宿主机虚拟化能力组成。

## 后端

后端入口在 `backend/main.go`，HTTP 服务路由集中在 `backend/internal/server/server.go`。主要模块：

- `internal/api`：Web 面板和 `/api/v1` 的 HTTP 接口。
- `internal/config`：配置和 SQLite 存储。
- `internal/lxc`：LXC 容器管理。
- `internal/kvm`：KVM/libvirt 虚拟机管理。
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
