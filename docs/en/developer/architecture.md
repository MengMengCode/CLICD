# Architecture

CLICD consists of a Go backend, a React frontend, and host virtualization capabilities.

## Backend

The backend entry point is `backend/main.go`, and HTTP routes are centralized in `backend/internal/server/server.go`. Main modules:

- `internal/api`: HTTP APIs for the web panel and `/api/v1`.
- `internal/config`: configuration and SQLite storage.
- `internal/lxc`: LXC container management.
- `internal/kvm`: KVM/libvirt virtual machine management.
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
