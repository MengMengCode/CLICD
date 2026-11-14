# Podman Containers

Use the Podman Containers page to deploy OCI applications such as Nginx. An application runs the process from its image and shares the host's Linux kernel. Use [LXC or KVM](./containers) for a full Linux environment, a system disk, or virtual hardware.

## Prepare the Host

Install Podman on the Linux host that runs CLICD, and verify that the CLICD service account can run it. With the default root installation, use `sudo podman info` on the host. On Debian or Ubuntu, install Podman from the distribution's package repository.

```bash
sudo apt install podman
sudo podman info
```

Sign into the panel as an administrator and open **Podman Containers**. This page manages the application containers created through the panel. By default, CLICD runs local Podman as root. Use it for images you trust. Sub-users, snapshots, and backups for LXC and KVM are managed on their existing pages.

## Deploy an Application

1. Click **Create Container** and enter a name and an OCI image with its registry, such as `docker.io/library/nginx:alpine`.
2. To reach the application through the host, publish a TCP or UDP port. For example, publish host port `8080` to container port `80`. Where network rules allow, open `http://<host-address>:8080` to reach Nginx.
3. Set CPU and memory limits as needed. Enter environment variables as one `KEY=value` per line. Supply command arguments as a JSON array of strings, such as `["nginx", "-g", "daemon off;"]`. The array replaces the image's default `CMD`. When the image defines an `ENTRYPOINT`, it receives the array as arguments. Check the image documentation for the expected arguments.
4. To persist data, enter one absolute path inside the container per line, such as `/var/cache/nginx`. Podman creates a volume for each path.

Ports are published on the host's IPv4 interfaces. Select host ports outside CLICD's configured [NAT4 port range](./networking#nat4) to avoid conflicts with LXC and KVM forwarding rules. Use the host or upstream firewall to restrict access where needed.

## Check Application Status

In **Applications**, search by name or image and filter by status. Start, stop, or restart a container from the list. Open its details to find CPU and memory limits, published ports, and volume paths under **Overview**. Running containers also show CPU, memory, network, and disk I/O. After an application exits, check its exit code.

## View Application Logs

Open **Logs** in the container details to see the application's standard output and standard error. Turn on **Auto-refresh**, or copy or download the logs currently shown. If an application exits immediately after starting, check the exit code and logs against the image's expected arguments.

## Manage Local Images

**Local images** shows the Podman cache for the CLICD service account. Search by tag or select **Pull image** to download an image using its fully qualified OCI reference. Select **Use image** next to a local tag to open the creation form with that cached image selected.

## Persistent Volumes

Podman volumes keep data across stops and restarts. Deleting a container removes its anonymous volumes. Back up application data you want to keep before deleting the container.
