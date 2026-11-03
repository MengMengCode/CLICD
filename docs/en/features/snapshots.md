# Snapshot Management

Instances with data disks cannot create/restore existing snapshots or enable scheduled snapshots. Detached disks count too. Disable an active snapshot schedule before adding a data disk. Use [full backups](./storage-backups) for rollback, migration or cloning that includes the system disk and all data disks.

Snapshots save the current state of a container so it can be rolled back before upgrades, configuration changes, or delivery.

## Global Overview

```http
GET /api/v1/snapshots
```

Use this endpoint to view snapshot summaries for all containers.

## Container Snapshots

```http
GET /api/v1/containers/{id}/snapshots
POST /api/v1/containers/{id}/snapshots
DELETE /api/v1/containers/{id}/snapshots/{snapshot_id}
POST /api/v1/containers/{id}/snapshots/{snapshot_id}/restore
```

Restoring a snapshot changes container state. In production, confirm that the current workload can be interrupted first.

## Scheduled Snapshots and Quotas

```http
POST /api/v1/containers/{id}/snapshots/schedule
PUT /api/v1/containers/{id}/snapshots/quota
```

Scheduled snapshots are useful for long-running containers. Quotas prevent snapshots from growing without limit and filling the host disk.
