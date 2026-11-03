# 快照管理

快照用于保存容器当前状态，方便在升级、变更配置或交付前回滚。

配置数据盘的实例不能创建或恢复旧快照，也不能启用计划快照；已断开的数据盘同样计入。添加数据盘前请先关闭现有计划快照。需要包含系统盘和全部数据盘的回滚、迁移或克隆时，请使用[完整备份](./storage-backups)。

## 全局总览

```http
GET /api/v1/snapshots
```

用于查看所有容器的快照概览。

## 容器快照

```http
GET /api/v1/containers/{id}/snapshots
POST /api/v1/containers/{id}/snapshots
DELETE /api/v1/containers/{id}/snapshots/{snapshot_id}
POST /api/v1/containers/{id}/snapshots/{snapshot_id}/restore
```

恢复快照会改变容器状态，生产环境建议先确认当前业务是否可以中断。

## 计划快照与配额

```http
POST /api/v1/containers/{id}/snapshots/schedule
PUT /api/v1/containers/{id}/snapshots/quota
```

计划快照适合长期运行的容器。配额用于避免快照无限增长占满宿主机磁盘。
