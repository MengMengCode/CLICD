# Tasks and Metric History

The Tasks page shows lifecycle operations, disk growth, data-disk changes, full backups, restore, clone and template operations with status, duration, errors and stage logs. Long changes for the same instance run sequentially.

```http
GET /api/v1/tasks
DELETE /api/v1/tasks/{task_id}
```

After a `202` response, find `data.task_id` in the task list. `done` indicates success; inspect `error` and stage logs for `failed` tasks. A running task cannot be removed. Deleting completed history does not undo disk, backup or instance changes.

## Service Restarts

- Task records and stage logs are stored in SQLite. Terminal history is retained for up to 30 days and 1,000 records, with up to 200 stage events per task.
- Pending generic operations return to the queue.
- Disk, backup, restore, clone and other generic operations that were running at restart become `failed/interrupted` and are not blindly replayed. Inspect actual resources before retrying.
- Sub-users only see tasks for assigned instances. Internal execution payloads and credentials are not exposed as task results.

## Resource History

Host and instance CPU, memory, network and disk I/O history is sampled approximately every 30 seconds and persisted for up to 7 days across service restarts. Downtime and periods when the service was not running are not backfilled.

Back up the panel database to preserve task and metric history. Full instance backups contain guest disks and instance settings and do not replace panel database backups.
