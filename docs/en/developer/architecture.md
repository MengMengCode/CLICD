# Architecture

CLICD consists of a Go backend, a React frontend, and host virtualization capabilities.

## Backend

The backend entry point is `backend/main.go`, and HTTP routes are centralized in `backend/internal/server/server.go`. Main modules:

- `internal/api`: HTTP APIs for the web panel and `/api/v1`.
- `internal/config`: configuration and SQLite storage.
- `internal/lxc`: LXC container management.
- `internal/kvm`: KVM/libvirt virtual machine management.
- `internal/transfer`: complete disk archives, validation, portable backups and instance templates.
- `internal/cli`: command-line management entry point.
- `internal/server`: embedded frontend assets and HTTP service.
- `internal/version`: version number.

## Frontend

The frontend entry point is `frontend/src/main.tsx`. Pages live in `frontend/src/pages`, and shared components live in `frontend/src/components`.

Main pages:

- Dashboard: `Dashboard.tsx`
- Container list: `Containers.tsx`
- Container details: `ContainerDetail.tsx`
- Image Management: `ImageManagement.tsx`
- Snapshot Management: `Snapshots.tsx`
- Task history: `Tasks.tsx`
- Full backups and instance templates: `PortableLibrary.tsx` (`/backups`, `/instance-templates`)
- Routing Management: `Routing.tsx`
- API Integration: `ApiIntegration.tsx`
- Host Report: `HostReport.tsx`
- Sub-user Management: `SubUserManagement.tsx`

## Frontend Embedding

For production builds, frontend artifacts are placed in `backend/internal/server/web`. The backend serves them through Go embed and returns the SPA entry for non-API routes.

## API Layers

- `/api/*`: web panel and compatibility APIs.
- `/api/v1/*`: versioned APIs recommended for external automation.
- WebSSH and WebVNC use short-lived tickets before opening WebSocket connections.

## Task and Storage State

Disk, backup, restore, clone and template tasks share per-instance queue locks. Independent SQLite history tables store task events/results and metrics sampled about every 30 seconds. Running generic operations become interrupted failures after service restart; pending operations return to the queue. Container metadata also persists data disks, standalone system disks and installation-media state.

KVM data disks are managed QCOW2 files included in domain XML. LXC uses ext4 images mounted outside rootfs with managed `lxc.mount.entry` records, keeping data disks out of rootfs cold copies. Full archives validate and independently copy every disk. Existing snapshots reject instances with data disks. See [storage limits](../features/storage-backups) and [task history](../features/tasks).
