# Dashboard

Host and instance metric history is sampled about every 30 seconds and persisted in SQLite for up to 7 days across service restarts. Task records and stage logs are persisted too; see [Tasks and Metric History](./tasks).

The dashboard shows the overall state of the host and virtualization resources.

## Metrics

- Total containers, running containers, and stopped containers.
- CPU, memory, disk, and Swap overview.
- Entry points for host network and routing status.
- Task queue status.

## Related APIs

```http
GET /api/v1/dashboard
GET /api/v1/host-info
GET /api/v1/routing
GET /api/v1/ipv6/status
GET /api/v1/tasks
```

API requests must include an API key:

```bash
curl -H "X-API-Key: YOUR_API_KEY" https://panel.example.com/api/v1/dashboard
```
