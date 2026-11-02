# Sub-users

Sub-users let administrators grant specific container access to other users. They are useful for temporary delivery, shared-host allocation, teaching labs, or multi-user host scenarios.

## Create an Access Link

After selecting a container, the administrator can create a sub-user link:

```http
POST /api/v1/sub-user/create
```

The response may include a username, initial password, access code, or access link. When sharing externally, mask sensitive values and send the real values only to the intended user.

## Manage Sub-users

```http
GET /api/v1/sub-users
POST /api/v1/sub-users/{id}/rotate-password
GET /api/v1/sub-users/{id}/audit-logs
GET /api/v1/sub-users/{id}/login-logs
```

Rotating the password invalidates old credentials. Audit logs and login logs help investigate mistakes or abnormal access.

## Permission Scope

Sub-users can only manage authorized containers. Global configuration, image management, API keys, and other administrator features are not exposed to sub-users.
