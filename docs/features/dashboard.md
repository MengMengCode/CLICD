# 控制面板

控制面板用于查看宿主机和虚拟化资源的整体状态。

## 统计项

- 容器总数、运行中数量和停止数量。
- CPU、内存、磁盘、Swap 等资源概览。
- 主机网络和路由状态入口。
- 任务队列状态。

## 相关接口

```http
GET /api/v1/dashboard
GET /api/v1/host-info
GET /api/v1/routing
GET /api/v1/ipv6/status
GET /api/v1/tasks
```

API 需要携带 API Key：

```bash
curl -H "X-API-Key: YOUR_API_KEY" https://panel.example.com/api/v1/dashboard
```
