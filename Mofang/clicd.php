<?php

use think\Db;

define('CLICD_DEBUG', false);

function clicd_debug($message, $data = null)
{
    if (!CLICD_DEBUG) {
        return;
    }
    $line = '[CLICD-DEBUG] ' . $message;
    if ($data !== null) {
        $line .= ' | ' . json_encode($data, JSON_UNESCAPED_UNICODE);
    }
    error_log($line);
}

function clicd_debug_entry($message, $data = null)
{
    return [
        'time'    => date('Y-m-d H:i:s'),
        'message' => $message,
        'data'    => $data,
    ];
}

function clicd_json_response($payload)
{
    if (!headers_sent()) {
        header('Content-Type: application/json; charset=utf-8');
    }
    echo json_encode($payload, JSON_UNESCAPED_UNICODE);
    exit;
}

function clicd_MetaData()
{
    return [
        'DisplayName' => 'CLICD 对接模块 by 欢-Huan and ChatGPT 5.5 and DeepSeek V4',
        'APIVersion'  => '1.1',
        'HelpDoc'     => 'https://github.com/MengMengCode/CLICD',
        'version'     => '1.0.11',
    ];
}

function clicd_ConfigOptions()
{
    return [
        ['type' => 'dropdown', 'name' => '虚拟化类型', 'description' => 'lxc 或 kvm', 'default' => 'lxc', 'key' => 'virtualization', 'options' => ['lxc' => 'LXC', 'kvm' => 'KVM']],
        ['type' => 'text', 'name' => '镜像/模板 ID', 'description' => 'CLICD 模板 ID，例如 alpine-3.21、debian-bookworm、ubuntu-jammy 或已启用的 KVM 镜像 ID', 'default' => 'alpine-3.21', 'key' => 'template_id'],
        ['type' => 'text', 'name' => 'CPU 核心', 'description' => 'vCPU 数量，KVM 必须为整数', 'default' => '1', 'key' => 'vcpu'],
        ['type' => 'text', 'name' => 'CPU 百分比', 'description' => 'CPU 使用率限制，0 表示不额外限制', 'default' => '0', 'key' => 'cpu_percent'],
        ['type' => 'text', 'name' => '内存 MB', 'description' => '容器内存，单位 MB', 'default' => '512', 'key' => 'ram_mb'],
        ['type' => 'text', 'name' => '硬盘 GB', 'description' => '系统盘大小，单位 GB（支持 0.5、0.75、1、5 等浮点数）', 'default' => '5', 'key' => 'disk_gb'],
        ['type' => 'text', 'name' => '带宽 Mbps', 'description' => '网络带宽限制，0 表示不限制', 'default' => '100', 'key' => 'network_bw_mbps'],
        ['type' => 'dropdown', 'name' => '流量模式', 'description' => 'total=总流量，in_out=分别限制入/出方向', 'default' => 'total', 'key' => 'traffic_mode', 'options' => ['total' => '总流量', 'in_out' => '入/出分开']],
        ['type' => 'text', 'name' => '月流量 GB', 'description' => 'total 模式下的月流量限制，0 表示不限制', 'default' => '100', 'key' => 'monthly_traffic_gb'],
        ['type' => 'text', 'name' => '入站流量 GB', 'description' => 'in_out 模式下入站流量限制，0 表示不限制', 'default' => '0', 'key' => 'traffic_in_gb'],
        ['type' => 'text', 'name' => '出站流量 GB', 'description' => 'in_out 模式下出站流量限制，0 表示不限制', 'default' => '0', 'key' => 'traffic_out_gb'],
        ['type' => 'text', 'name' => 'IO 速度 MB/s', 'description' => '磁盘 IO 限制，0 表示不限制', 'default' => '0', 'key' => 'io_speed_mbps'],
        ['type' => 'dropdown', 'name' => '分配 NAT', 'description' => '开通时是否分配 NAT 端口映射', 'default' => 'true', 'key' => 'assign_nat', 'options' => ['true' => '启用', 'false' => '禁用']],
        ['type' => 'text', 'name' => 'NAT 端口数量', 'description' => '开通时分配的端口映射数量，最小 2', 'default' => '2', 'key' => 'port_mapping_count'],
        ['type' => 'text', 'name' => '快照配额', 'description' => '每台实例允许保留的快照数量', 'default' => '3', 'key' => 'snapshot_limit'],
        ['type' => 'text', 'name' => '额外端口', 'description' => '逗号分隔的容器端口，例如 80,443', 'default' => '', 'key' => 'extra_ports'],
        ['type' => 'dropdown', 'name' => '自动公网 IPv4', 'description' => '开通时是否从 CLICD 公网 IPv4 池分配独立 IPv4', 'default' => 'false', 'key' => 'assign_ipv4', 'options' => ['true' => '启用', 'false' => '禁用']],
        ['type' => 'text', 'name' => '公网 IPv4 数量', 'description' => '自动分配公网 IPv4 的数量，通常填写 1', 'default' => '1', 'key' => 'ipv4_count'],
        ['type' => 'text', 'name' => '指定公网 IPv4', 'description' => '指定分配的公网 IPv4，多个用逗号分隔；留空则从地址池自动分配', 'default' => '', 'key' => 'public_ipv4s'],
        ['type' => 'dropdown', 'name' => '自动 IPv6', 'description' => '开通时自动分配 IPv6', 'default' => 'false', 'key' => 'assign_ipv6', 'options' => ['true' => '启用', 'false' => '禁用']],
        ['type' => 'text', 'name' => 'IPv6 数量', 'description' => '自动分配 IPv6 的数量，通常填写 1', 'default' => '1', 'key' => 'ipv6_count'],
        ['type' => 'text', 'name' => '指定 IPv6', 'description' => '指定分配的 IPv6 地址，多个用逗号分隔；留空则从地址池自动分配', 'default' => '', 'key' => 'ipv6_addresses'],
        ['type' => 'dropdown', 'name' => 'SSH 鉴权模式', 'description' => 'auto_password=自动生成密码，password=使用指定密码，key=使用 SSH 公钥', 'default' => 'auto_password', 'key' => 'ssh_auth_mode', 'options' => ['auto_password' => '自动密码', 'password' => '指定密码', 'key' => 'SSH 公钥']],
        ['type' => 'text', 'name' => '指定 SSH 密码', 'description' => 'SSH 鉴权模式为 password 时使用；其他模式留空', 'default' => '', 'key' => 'ssh_password'],
        ['type' => 'text', 'name' => 'SSH 公钥', 'description' => 'SSH 鉴权模式为 key 时使用；填写完整 public key', 'default' => '', 'key' => 'ssh_public_key'],
        ['type' => 'dropdown', 'name' => '同步到期时间', 'description' => '开通/续费时把魔方到期日期同步到 CLICD，格式会转换为 YYYY-MM-DD', 'default' => 'true', 'key' => 'sync_expiry', 'options' => ['true' => '启用', 'false' => '禁用']],
    ];
}

function clicd_base_url($params)
{
    if (!empty($params['server_host'])) {
        return rtrim($params['server_host'], '/');
    }

    $host = $params['server_ip'] ?? $params['ip'] ?? '';
    $port = $params['port'] ?? '';
    $scheme = (!empty($params['secure']) && (string)$params['secure'] !== '0') ? 'https' : 'http';

    if (stripos($host, 'http://') === 0 || stripos($host, 'https://') === 0) {
        $base = rtrim($host, '/');
    } else {
        $base = $scheme . '://' . $host;
    }

    if ($port !== '' && strpos(parse_url($base, PHP_URL_HOST) ?: $base, ':') === false) {
        $base .= ':' . $port;
    }

    return rtrim($base, '/');
}

function clicd_api_key($params)
{
    foreach (['accesshash', 'server_password', 'password'] as $key) {
        if (!empty($params[$key])) {
            return trim($params[$key]);
        }
    }
    return '';
}

function clicd_request($params, $endpoint, $data = [], $method = 'GET', $timeout = 30)
{
    $url = clicd_base_url($params) . $endpoint;
    $apiKey = clicd_api_key($params);
    $method = strtoupper($method);

    $curl = curl_init();
    $headers = [
        'Content-Type: application/json',
        'X-API-Key: ' . $apiKey,
        'Authorization: Bearer ' . $apiKey,
    ];

    $options = [
        CURLOPT_URL            => $url,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT        => $timeout,
        CURLOPT_CONNECTTIMEOUT => 10,
        CURLOPT_FOLLOWLOCATION => true,
        CURLOPT_CUSTOMREQUEST  => $method,
        CURLOPT_HTTPHEADER     => $headers,
        CURLOPT_SSL_VERIFYPEER => false,
        CURLOPT_SSL_VERIFYHOST => false,
        CURLOPT_USERAGENT      => 'Mofang-CLICD',
    ];

    if ($method !== 'GET' && $data !== null) {
        $options[CURLOPT_POSTFIELDS] = json_encode($data, JSON_UNESCAPED_UNICODE);
    }

    curl_setopt_array($curl, $options);
    $body = curl_exec($curl);
    $errno = curl_errno($curl);
    $error = curl_error($curl);
    $httpCode = curl_getinfo($curl, CURLINFO_HTTP_CODE);
    curl_close($curl);

    clicd_debug('request', ['url' => $url, 'method' => $method, 'http_code' => $httpCode, 'errno' => $errno]);

    if ($errno) {
        return ['success' => false, 'message' => 'CURL ERROR: ' . $error, '_http_code' => 0];
    }

    $decoded = json_decode($body, true);
    if (!is_array($decoded)) {
        return ['success' => false, 'message' => 'Invalid JSON response: ' . substr((string)$body, 0, 300), '_http_code' => $httpCode];
    }

    $decoded['_http_code'] = $httpCode;
    return $decoded;
}

function clicd_request_debug($params, $endpoint, $data = [], $method = 'GET', $timeout = 30)
{
    $started = microtime(true);
    $res = clicd_request($params, $endpoint, $data, $method, $timeout);
    return [
        'response' => $res,
        'debug'    => clicd_debug_entry('CLICD API request', [
            'method'   => strtoupper($method),
            'endpoint' => $endpoint,
            'payload'  => $data,
            'http'     => is_array($res) ? ($res['_http_code'] ?? null) : null,
            'success'  => clicd_success($res),
            'message'  => clicd_message($res, ''),
            'ms'       => (int)round((microtime(true) - $started) * 1000),
        ]),
    ];
}

function clicd_success($res)
{
    if (!is_array($res)) {
        return false;
    }
    if (isset($res['success'])) {
        return (bool)$res['success'];
    }
    return isset($res['code']) && (int)$res['code'] >= 200 && (int)$res['code'] < 300;
}

function clicd_message($res, $fallback = '操作失败')
{
    if (!is_array($res)) {
        return $fallback;
    }
    return $res['message'] ?? $res['msg'] ?? $res['error'] ?? $fallback;
}

function clicd_container_name($params)
{
    $name = $params['domain'] ?? '';
    if (is_array($name)) {
        $name = reset($name);
    }
    $name = trim((string)$name);
    if ($name === '') {
        $name = 'host-' . ($params['hostid'] ?? time());
    }
    $name = preg_replace('/[^A-Za-z0-9_.-]/', '-', $name);
    return trim($name, '-.');
}

function clicd_host_id($params)
{
    foreach (['hostid', 'id', 'serviceid', 'service_id', 'relid'] as $key) {
        if (!empty($params[$key]) && is_numeric($params[$key])) {
            return (int)$params[$key];
        }
    }
    return 0;
}
function clicd_first_string($value)
{
    if (is_array($value)) {
        foreach ($value as $item) {
            if (is_array($item)) {
                foreach (['address', 'ip', 'ipv4', 'public_ip', 'public_ipv4'] as $key) {
                    if (!empty($item[$key])) {
                        $itemValue = trim((string)$item[$key]);
                        if ($itemValue !== '') {
                            return $itemValue;
                        }
                    }
                }
                continue;
            }

            $itemValue = trim((string)$item);
            if ($itemValue !== '') {
                return $itemValue;
            }
        }
        return '';
    }

    $value = trim((string)$value);
    return $value;
}

function clicd_public_host_from_container($container = [])
{
    if (is_array($container)) {
        foreach (['public_ipv4s', 'public_ipv4', 'public_ip', 'ipv4_addresses', 'ipv4', 'nat_public_ip', 'host_ip', 'external_ip', 'node_ip', 'nat_host'] as $key) {
            if (!empty($container[$key])) {
                $value = clicd_first_string($container[$key]);
                if ($value !== '') {
                    return $value;
                }
            }
        }
    }

    return '';
}

function clicd_public_ipv4_from_routing($params, $container = [])
{
    if (!is_array($container)) {
        return '';
    }

    $containerId = isset($container['id']) ? (string)$container['id'] : '';
    $containerName = isset($container['name']) ? (string)$container['name'] : clicd_container_name($params);

    $res = clicd_request($params, '/api/v1/routing', [], 'GET', 30);
    if (!clicd_success($res) || empty($res['data']['ipv4_assignments']) || !is_array($res['data']['ipv4_assignments'])) {
        return '';
    }

    foreach ($res['data']['ipv4_assignments'] as $assignment) {
        if (!is_array($assignment)) {
            continue;
        }
        $matchId = $containerId !== '' && isset($assignment['container_id']) && (string)$assignment['container_id'] === $containerId;
        $matchName = $containerName !== '' && isset($assignment['container_name']) && (string)$assignment['container_name'] === $containerName;
        if ($matchId || $matchName) {
            return clicd_first_string($assignment['address'] ?? '');
        }
    }

    return '';
}

function clicd_public_host($params, $container = [], $useRouting = false)
{
    $fromContainer = clicd_public_host_from_container($container);
    if ($fromContainer !== '') {
        return $fromContainer;
    }

    if ($useRouting) {
        $fromRouting = clicd_public_ipv4_from_routing($params, $container);
        if ($fromRouting !== '') {
            return $fromRouting;
        }
    }

    foreach (['server_ip', 'ip'] as $key) {
        if (!empty($params[$key])) {
            $value = trim((string)$params[$key]);
            if (stripos($value, 'http://') === 0 || stripos($value, 'https://') === 0) {
                return parse_url($value, PHP_URL_HOST) ?: $value;
            }
            return $value;
        }
    }

    return parse_url(clicd_base_url($params), PHP_URL_HOST) ?: '';
}

function clicd_container_ssh_port($container)
{
    if (!is_array($container)) {
        return '';
    }
    foreach (['ssh_port', 'host_ssh_port', 'nat_ssh_port'] as $key) {
        if (isset($container[$key]) && $container[$key] !== '') {
            return (int)$container[$key];
        }
    }
    return '';
}

function clicd_container_password($container)
{
    if (!is_array($container)) {
        return '';
    }
    foreach (['ssh_password', 'password', 'root_password', 'default_password'] as $key) {
        if (isset($container[$key]) && $container[$key] !== '') {
            $password = trim((string)$container[$key]);
            if ($password !== '' && !preg_match('/^\*+$/', $password)) {
                return $password;
            }
        }
    }
    return '';
}

function clicd_store_password($password)
{
    $password = (string)$password;
    if ($password === '') {
        return '';
    }
    return function_exists('cmf_encrypt') ? cmf_encrypt($password) : $password;
}

function clicd_webssh_url($params, $ticket, $containerName)
{
    $baseUrl = rtrim(clicd_base_url($params), '/');
    $scheme = stripos($baseUrl, 'https://') === 0 ? 'wss' : 'ws';
    $host = parse_url($baseUrl, PHP_URL_HOST);
    $port = parse_url($baseUrl, PHP_URL_PORT);
    $wsBase = $scheme . '://' . $host . ($port ? ':' . $port : '');
    $wsUrl = $wsBase
        . '/api/ssh?container=' . rawurlencode((string)$containerName)
        . '&container_name=' . rawurlencode((string)$containerName)
        . '&ticket=' . rawurlencode((string)$ticket);

    $siteScheme = (!empty($_SERVER['HTTPS']) && $_SERVER['HTTPS'] !== 'off') ? 'https' : 'http';
    $siteHost = $_SERVER['HTTP_HOST'] ?? '';
    $handler = ($siteHost !== '' ? $siteScheme . '://' . $siteHost : '') . '/plugins/servers/clicd/handlers/webssh.php';

    return $handler
        . '?ws=' . rawurlencode($wsUrl)
        . '&protocol=' . rawurlencode('clicd-ticket.' . (string)$ticket)
        . '&ticket=' . rawurlencode((string)$ticket)
        . '&container=' . rawurlencode((string)$containerName);
}

function clicd_bool_option($value, $default = false)
{
    if ($value === null || $value === '') {
        return $default;
    }
    if (is_bool($value)) {
        return $value;
    }
    return in_array(strtolower((string)$value), ['1', 'true', 'yes', 'on'], true);
}

function clicd_int_option($options, $key, $default = 0)
{
    if (!isset($options[$key]) || $options[$key] === '') {
        return $default;
    }
    return (int)$options[$key];
}

function clicd_float_option($options, $key, $default = 0)
{
    if (!isset($options[$key]) || $options[$key] === '') {
        return $default;
    }
    return (float)$options[$key];
}

function clicd_number_value($value, $default = 0)
{
    if (is_numeric($value)) {
        return (float)$value;
    }
    if (is_string($value) && preg_match('/-?\d+(?:\.\d+)?/', $value, $match)) {
        return (float)$match[0];
    }
    return $default;
}

function clicd_pick_number($sources, $keys, $default = 0)
{
    foreach ($sources as $source) {
        if (!is_array($source)) {
            continue;
        }
        foreach ($keys as $key) {
            if (array_key_exists($key, $source) && $source[$key] !== '' && $source[$key] !== null) {
                return clicd_number_value($source[$key], $default);
            }
        }
    }
    return $default;
}

function clicd_pct($value)
{
    $value = clicd_number_value($value, 0);
    if ($value < 0) {
        return 0;
    }
    if ($value > 100) {
        return 100;
    }
    return round($value, 2);
}

function clicd_bytes_to_gb($bytes)
{
    return round(clicd_number_value($bytes, 0) / 1073741824, 2);
}

function clicd_bytes_to_mb($bytes)
{
    return round(clicd_number_value($bytes, 0) / 1048576, 2);
}

function clicd_format_bytes($bytes)
{
    $value = clicd_number_value($bytes, 0);
    if ($value >= 1073741824) {
        return round($value / 1073741824, 2) . ' GB';
    }
    if ($value >= 1048576) {
        return round($value / 1048576, 2) . ' MB';
    }
    if ($value >= 1024) {
        return round($value / 1024, 2) . ' KB';
    }
    return round($value, 2) . ' B';
}

function clicd_format_rate($bytesPerSecond)
{
    $value = clicd_number_value($bytesPerSecond, 0);
    if ($value >= 1073741824) {
        return round($value / 1073741824, 2) . ' GB/s';
    }
    if ($value >= 1048576) {
        return round($value / 1048576, 2) . ' MB/s';
    }
    if ($value >= 1024) {
        return round($value / 1024, 2) . ' KB/s';
    }
    return round($value, 2) . ' B/s';
}

function clicd_extra_ports($value)
{
    if (empty($value)) {
        return [];
    }
    $ports = [];
    foreach (preg_split('/[,;\s]+/', (string)$value) as $port) {
        $port = (int)trim($port);
        if ($port > 0 && $port <= 65535) {
            $ports[] = $port;
        }
    }
    return array_values(array_unique($ports));
}

function clicd_csv_values($value)
{
    if (is_array($value)) {
        $parts = $value;
    } else {
        $parts = preg_split('/[,;\s]+/', (string)$value);
    }

    $result = [];
    foreach ($parts as $part) {
        $part = trim((string)$part);
        if ($part !== '') {
            $result[] = $part;
        }
    }
    return array_values(array_unique($result));
}

function clicd_expiry_from_params($params)
{
    $options = $params['configoptions'] ?? [];
    if (!clicd_bool_option($options['sync_expiry'] ?? 'true', true)) {
        return '';
    }
    $raw = $params['nextduedate'] ?? '';
    if ($raw === '' || $raw === '0' || $raw === 0 || $raw === '0000-00-00' || $raw === '0000-00-00 00:00:00') {
        return '';
    }

    $timestamp = 0;
    if (is_numeric($raw)) {
        $timestamp = (int)$raw;
        if ($timestamp > 20000000000) {
            $timestamp = (int)floor($timestamp / 1000);
        }
    } else {
        $timestamp = strtotime((string)$raw);
    }

    if ($timestamp === false || $timestamp <= time()) {
        return '';
    }

    return date('Y-m-d', $timestamp);
}

function clicd_container_payload($params)
{
    $options = $params['configoptions'] ?? [];
    $trafficMode = $options['traffic_mode'] ?? 'total';
    $assignNat = clicd_bool_option($options['assign_nat'] ?? 'true', true);
    $assignIpv4 = clicd_bool_option($options['assign_ipv4'] ?? 'false', false);
    $assignIpv6 = clicd_bool_option($options['assign_ipv6'] ?? 'false', false);
    $publicIpv4s = clicd_csv_values($options['public_ipv4s'] ?? '');
    $ipv6Addresses = clicd_csv_values($options['ipv6_addresses'] ?? '');
    if (!empty($publicIpv4s)) {
        $assignIpv4 = true;
    }
    if (!empty($ipv6Addresses)) {
        $assignIpv6 = true;
    }
    $sshAuthMode = strtolower(trim((string)($options['ssh_auth_mode'] ?? 'auto_password')));
    if (!in_array($sshAuthMode, ['auto_password', 'password', 'key'], true)) {
        $sshAuthMode = 'auto_password';
    }

    return [
        'name'               => clicd_container_name($params),
        'virtualization'     => $options['virtualization'] ?? 'lxc',
        'template_id'        => $options['template_id'] ?? '',
        'vcpu'               => clicd_float_option($options, 'vcpu', 1),
        'cpu_percent'        => clicd_int_option($options, 'cpu_percent', 0),
        'ram_mb'             => clicd_int_option($options, 'ram_mb', 512),
        'disk_gb'            => clicd_float_option($options, 'disk_gb', 5),
        'network_bw_mbps'    => clicd_int_option($options, 'network_bw_mbps', 100),
        'monthly_traffic_gb' => clicd_int_option($options, 'monthly_traffic_gb', 100),
        'traffic_mode'       => in_array($trafficMode, ['total', 'in_out'], true) ? $trafficMode : 'total',
        'traffic_in_gb'      => clicd_int_option($options, 'traffic_in_gb', 0),
        'traffic_out_gb'     => clicd_int_option($options, 'traffic_out_gb', 0),
        'io_speed_mbps'      => clicd_int_option($options, 'io_speed_mbps', 0),
        'extra_ports'        => clicd_extra_ports($options['extra_ports'] ?? ''),
        'port_mapping_count' => $assignNat ? max(2, clicd_int_option($options, 'port_mapping_count', 2)) : 0,
        'assign_nat'         => $assignNat,
        'assign_ipv4'        => $assignIpv4,
        'ipv4_count'         => max(1, clicd_int_option($options, 'ipv4_count', 1)),
        'public_ipv4s'       => $publicIpv4s,
        'snapshot_limit'     => max(1, clicd_int_option($options, 'snapshot_limit', 3)),
        'assign_ipv6'        => $assignIpv6,
        'ipv6_count'         => max(1, clicd_int_option($options, 'ipv6_count', 1)),
        'ipv6_addresses'     => $ipv6Addresses,
        'ssh_auth_mode'      => $sshAuthMode,
        'ssh_password'       => (string)($options['ssh_password'] ?? ''),
        'ssh_public_key'     => trim((string)($options['ssh_public_key'] ?? '')),
        'expires_at'         => clicd_expiry_from_params($params),
    ];
}

function clicd_find_container($params)
{
    $name = clicd_container_name($params);
    return clicd_request($params, '/api/v1/containers/' . rawurlencode($name), [], 'GET');
}

function clicd_post_value($key, $default = '')
{
    if (function_exists('input')) {
        $value = input('post.' . $key);
        return $value === null ? $default : $value;
    }
    return $_POST[$key] ?? $default;
}

function clicd_request_value($key, $default = '')
{
    if (function_exists('input')) {
        $value = input('param.' . $key);
        if ($value === null) {
            $value = input('*.' . $key);
        }
        return $value === null ? $default : $value;
    }
    if (isset($_POST[$key])) {
        return $_POST[$key];
    }
    return $_GET[$key] ?? $default;
}

function clicd_json_input()
{
    $input = [];
    if (!empty($_POST) && is_array($_POST)) {
        $input = $_POST;
    }

    $raw = file_get_contents('php://input');
    $data = json_decode((string)$raw, true);
    if (is_array($data)) {
        return array_merge($input, $data);
    }

    $form = [];
    parse_str((string)$raw, $form);
    if (!empty($form) && is_array($form)) {
        return array_merge($input, $form);
    }

    return $input;
}

function clicd_param_value($data, $key, $default = '')
{
    if (is_array($data) && array_key_exists($key, $data)) {
        return $data[$key];
    }
    return clicd_request_value($key, $default);
}

function clicd_container_api_id($params, &$container = null)
{
    $res = clicd_find_container($params);
    if (clicd_success($res) && !empty($res['data']) && is_array($res['data'])) {
        $container = $res['data'];
        if (!empty($container['id'])) {
            return (string)$container['id'];
        }
        if (!empty($container['uuid'])) {
            return (string)$container['uuid'];
        }
        if (!empty($container['name'])) {
            return (string)$container['name'];
        }
    }

    $container = [];
    return clicd_container_name($params);
}

function clicd_port_mappings_from_container($container)
{
    if (!is_array($container)) {
        return [];
    }

    foreach (['port_mappings', 'portMappings', 'nat', 'nat_list', 'NatList'] as $key) {
        if (!empty($container[$key]) && is_array($container[$key])) {
            return $container[$key];
        }
    }

    return [];
}

function clicd_normalize_port_mappings($mappings)
{
    if (!is_array($mappings)) {
        return [];
    }

    $result = [];
    foreach ($mappings as $index => $mapping) {
        if (!is_array($mapping)) {
            continue;
        }
        $protocol = strtolower((string)($mapping['protocol'] ?? 'tcp'));
        $result[] = [
            'index'          => is_numeric($index) ? (int)$index : $index,
            'host_port'      => $mapping['host_port'] ?? '',
            'container_port' => $mapping['container_port'] ?? '',
            'protocol'       => in_array($protocol, ['tcp', 'udp'], true) ? $protocol : 'tcp',
            'tcp_selected'   => $protocol === 'udp' ? '' : 'selected',
            'udp_selected'   => $protocol === 'udp' ? 'selected' : '',
            'description'    => $mapping['description'] ?? '',
        ];
    }

    return $result;
}

function clicd_nat_post_action()
{
    $func = clicd_request_value('func', '');
    return strtolower(trim((string)$func));
}

function clicd_handle_nat_post($params)
{
    $action = clicd_nat_post_action();
    if ($action === '') {
        return ['message' => '', 'mappings' => null];
    }

    if (!in_array($action, ['randomport', 'addnat', 'updatenat', 'deletenat'], true)) {
        return ['message' => '', 'mappings' => null];
    }

    $map = [
        'randomport' => 'clicd_randomPort',
        'addnat'     => 'clicd_addNat',
        'updatenat'  => 'clicd_updateNat',
        'deletenat'  => 'clicd_deleteNat',
    ];

    if (!isset($map[$action]) || !function_exists($map[$action])) {
        return ['message' => '', 'mappings' => null];
    }

    $res = call_user_func($map[$action], $params);
    if (!is_array($res)) {
        return ['message' => (string)$res, 'mappings' => null];
    }

    $ok = (($res['status'] ?? '') === 'success' || (int)($res['status'] ?? 0) === 200);
    $prefix = $ok ? '成功: ' : '失败: ';
    return [
        'message'  => $prefix . ($res['msg'] ?? '操作完成'),
        'mappings' => ($ok && isset($res['data']['port_mappings']) && is_array($res['data']['port_mappings'])) ? $res['data']['port_mappings'] : null,
    ];
}

function clicd_nat_payload_from_post()
{
    return clicd_nat_payload_from_data(null);
}

function clicd_nat_payload_from_data($data = null)
{
    $hostPort = (int)clicd_param_value($data, 'host_port', 0);
    $containerPort = (int)clicd_param_value($data, 'container_port', 0);
    $protocol = strtolower(trim((string)clicd_param_value($data, 'protocol', 'tcp')));
    $description = trim((string)clicd_param_value($data, 'description', ''));

    if ($hostPort < 1 || $hostPort > 65535) {
        return ['error' => '公网端口必须在 1-65535 之间'];
    }
    if ($containerPort < 1 || $containerPort > 65535) {
        return ['error' => '容器端口必须在 1-65535 之间'];
    }
    if (!in_array($protocol, ['tcp', 'udp'], true)) {
        return ['error' => '协议只支持 tcp 或 udp'];
    }

    return [
        'container_port' => $containerPort,
        'host_port'      => $hostPort,
        'protocol'       => $protocol,
        'description'    => $description,
    ];
}

function clicd_nat_ajax($params)
{
    $input = clicd_json_input();
    $action = strtolower(trim((string)clicd_param_value($input, 'action', '')));
    $debug = [clicd_debug_entry('NAT ajax received', [
        'action' => $action,
        'input'  => $input,
        'query'  => $_GET,
    ])];

    $container = [];
    $containerId = clicd_container_api_id($params, $container);
    $debug[] = clicd_debug_entry('Container resolved', [
        'container_id' => $containerId,
        'container'    => [
            'id'   => $container['id'] ?? null,
            'uuid' => $container['uuid'] ?? null,
            'name' => $container['name'] ?? null,
        ],
    ]);

    if (!in_array($action, ['random-port', 'add', 'update', 'delete'], true)) {
        return ['status' => 'error', 'msg' => '未知 NAT 操作', 'debug' => $debug];
    }

    if ($action === 'random-port') {
        $call = clicd_request_debug($params, '/api/v1/containers/' . rawurlencode($containerId) . '/random-port', [], 'GET', 30);
        $debug[] = $call['debug'];
        $res = $call['response'];
        return clicd_success($res)
            ? ['status' => 'success', 'msg' => '随机端口: ' . ($res['data']['port'] ?? ''), 'port' => $res['data']['port'] ?? '', 'debug' => $debug]
            : ['status' => 'error', 'msg' => clicd_message($res, '获取随机端口失败'), 'debug' => $debug];
    }

    if ($action === 'delete') {
        $index = clicd_param_value($input, 'index', '');
        if ($index === '' || !is_numeric($index) || (int)$index < 0) {
            return ['status' => 'error', 'msg' => '端口映射索引错误', 'debug' => $debug];
        }
        $endpoint = '/api/v1/containers/' . rawurlencode($containerId) . '/port-mappings/' . rawurlencode((string)(int)$index);
        $call = clicd_request_debug($params, $endpoint, [], 'DELETE', 30);
        $debug[] = $call['debug'];
        $res = $call['response'];
    } else {
        $payload = clicd_nat_payload_from_data($input);
        if (isset($payload['error'])) {
            return ['status' => 'error', 'msg' => $payload['error'], 'debug' => $debug];
        }

        if ($action === 'add') {
            $endpoint = '/api/v1/containers/' . rawurlencode($containerId) . '/port-mappings';
            $call = clicd_request_debug($params, $endpoint, $payload, 'POST', 30);
        } else {
            $index = clicd_param_value($input, 'index', '');
            if ($index === '' || !is_numeric($index) || (int)$index < 0) {
                return ['status' => 'error', 'msg' => '端口映射索引错误', 'debug' => $debug];
            }
            $endpoint = '/api/v1/containers/' . rawurlencode($containerId) . '/port-mappings/' . rawurlencode((string)(int)$index);
            $call = clicd_request_debug($params, $endpoint, $payload, 'PUT', 30);
        }
        $debug[] = $call['debug'];
        $res = $call['response'];
    }

    if (!clicd_success($res)) {
        return ['status' => 'error', 'msg' => clicd_message($res, 'NAT 操作失败'), 'debug' => $debug];
    }

    return [
        'status'        => 'success',
        'msg'           => clicd_message($res, 'NAT 操作成功'),
        'port_mappings' => clicd_normalize_port_mappings($res['data'] ?? []),
        'debug'         => $debug,
    ];
}

function clicd_info_ajax($params)
{
    $debug = [clicd_debug_entry('Info ajax received', ['query' => $_GET])];
    $res = clicd_find_container($params);
    if (!clicd_success($res) || empty($res['data']) || !is_array($res['data'])) {
        return ['status' => 'error', 'msg' => clicd_message($res, '获取实例信息失败'), 'debug' => $debug];
    }

    $c = $res['data'];
    $name = $c['name'] ?? clicd_container_name($params);

    $usageCall = clicd_request_debug($params, '/api/v1/containers/' . rawurlencode($name) . '/usage', [], 'GET', 30);
    if (!clicd_success($usageCall['response']) && !empty($c['uuid'])) {
        $usageCall = clicd_request_debug($params, '/api/containers/' . rawurlencode((string)$c['uuid']) . '/usage', [], 'GET', 30);
    }
    $trafficCall = clicd_request_debug($params, '/api/v1/containers/' . rawurlencode($name) . '/traffic', [], 'GET', 30);
    $debug[] = $usageCall['debug'];
    $debug[] = $trafficCall['debug'];

    $usageRes = $usageCall['response'];
    $usage = clicd_success($usageRes) && isset($usageRes['data']) && is_array($usageRes['data']) ? $usageRes['data'] : [];
    $trafficRes = $trafficCall['response'];
    $traffic = clicd_success($trafficRes) && isset($trafficRes['data']) && is_array($trafficRes['data']) ? $trafficRes['data'] : [];
    $options = $params['configoptions'] ?? [];
    $sources = [$usage, $traffic, $c];

    $rxBytes = clicd_pick_number($sources, ['rx_used_bytes', 'rx_bytes', 'traffic_used_rx', 'in_bytes', 'input_bytes', 'network_rx_bytes'], 0);
    $txBytes = clicd_pick_number($sources, ['tx_used_bytes', 'tx_bytes', 'traffic_used_tx', 'out_bytes', 'output_bytes', 'network_tx_bytes'], 0);
    $totalBytes = clicd_pick_number($sources, ['total_used_bytes', 'traffic_used_bytes'], 0);
    if ($rxBytes <= 0 && $txBytes <= 0 && $totalBytes > 0) {
        $txBytes = $totalBytes;
    }
    if ($rxBytes <= 0) {
        $rxBytes = clicd_pick_number($sources, ['traffic_in_gb', 'in_gb', 'rx_gb'], 0) * 1073741824;
    }
    if ($txBytes <= 0) {
        $txBytes = clicd_pick_number($sources, ['traffic_out_gb', 'out_gb', 'tx_gb'], 0) * 1073741824;
    }

    $limitGB = clicd_pick_number([$traffic, $c, $options], ['monthly_traffic_gb', 'traffic_limit_gb', 'limit_gb'], 0);
    $trafficUsedGB = round(($rxBytes + $txBytes) / 1073741824, 2);
    $trafficPercent = $limitGB > 0 ? clicd_pct(($trafficUsedGB / $limitGB) * 100) : 0;

    $cpuPercent = clicd_pct(clicd_pick_number([$usage, $traffic], ['cpu_usage_pct', 'cpu_percent', 'cpu_usage_percent', 'cpu_usage', 'cpu'], 0));

    $memoryUsedMB = clicd_pick_number($sources, ['memory_used_mb', 'mem_used_mb', 'ram_used_mb', 'memory_usage_mb'], 0);
    if ($memoryUsedMB <= 0) {
        $memoryUsedMB = clicd_bytes_to_mb(clicd_pick_number($sources, ['memory_usage_bytes', 'memory_used', 'mem_used', 'ram_used', 'memory_bytes'], 0));
    }
    $memoryTotalMB = clicd_pick_number([$usage, $c, $options], ['memory_total_mb', 'mem_total_mb', 'ram_total_mb', 'ram_mb'], 0);
    if ($memoryTotalMB <= 0) {
        $memoryTotalMB = clicd_bytes_to_mb(clicd_pick_number($sources, ['memory_total', 'mem_total', 'ram_total'], 0));
    }
    $memoryPercent = $memoryTotalMB > 0 ? clicd_pct(($memoryUsedMB / $memoryTotalMB) * 100) : 0;

    $vcpu = clicd_pick_number([$c, $options], ['vcpu', 'cpu', 'cores'], 1);
    $loadPercent = clicd_pick_number([$usage, $traffic], ['load_percent', 'load_usage_percent'], -1);
    if ($loadPercent < 0) {
        $loadValue = clicd_pick_number([$usage, $traffic], ['load1', 'load', 'load_average'], 0);
        $loadPercent = $vcpu > 0 ? ($loadValue / $vcpu) * 100 : 0;
    }
    $loadPercent = clicd_pct($loadPercent);

    $diskUsedGB = clicd_pick_number($sources, ['disk_used_gb', 'disk_usage_gb', 'storage_used_gb'], 0);
    if ($diskUsedGB <= 0) {
        $diskUsedGB = clicd_bytes_to_gb(clicd_pick_number($sources, ['disk_usage_bytes', 'disk_used', 'disk_usage', 'storage_used'], 0));
    }
    $diskTotalGB = clicd_pick_number([$usage, $c, $options], ['disk_total_gb', 'storage_total_gb', 'disk_gb'], 0);
    if ($diskTotalGB <= 0) {
        $diskTotalGB = clicd_bytes_to_gb(clicd_pick_number($sources, ['disk_total', 'storage_total'], 0));
    }
    $diskPercent = $diskTotalGB > 0 ? clicd_pct(($diskUsedGB / $diskTotalGB) * 100) : 0;

    $netInBps = clicd_pick_number($sources, ['rx_bps', 'in_bps', 'network_rx_bps', 'net_in_bps'], 0);
    $netOutBps = clicd_pick_number($sources, ['tx_bps', 'out_bps', 'network_tx_bps', 'net_out_bps'], 0);
    $diskReadBps = clicd_pick_number($sources, ['disk_read_bps', 'read_bps', 'io_read_bps'], 0);
    $diskWriteBps = clicd_pick_number($sources, ['disk_write_bps', 'write_bps', 'io_write_bps'], 0);

    return [
        'status' => 'success',
        'data'   => [
            'cpu_percent'    => $cpuPercent,
            'cpu_detail'     => $cpuPercent . '%',
            'mem_percent'    => $memoryPercent,
            'mem_detail'     => ($memoryTotalMB > 0 ? round($memoryUsedMB, 0) . ' / ' . round($memoryTotalMB, 0) . ' MB' : '-'),
            'load_percent'   => $loadPercent,
            'load_detail'    => $loadPercent . '%',
            'disk_percent'   => $diskPercent,
            'disk_detail'    => ($diskTotalGB > 0 ? round($diskUsedGB, 2) . ' / ' . round($diskTotalGB, 2) . ' GB' : '-'),
            'traffic_used'   => $trafficUsedGB,
            'traffic_limit'  => $limitGB,
            'traffic_in_gb'  => round($rxBytes / 1073741824, 2),
            'traffic_out_gb' => round($txBytes / 1073741824, 2),
            'traffic_used_text' => clicd_format_bytes($rxBytes + $txBytes),
            'traffic_limit_text'=> $limitGB > 0 ? round($limitGB, 2) . ' GB' : '不限',
            'traffic_in_text'   => clicd_format_bytes($rxBytes),
            'traffic_out_text'  => clicd_format_bytes($txBytes),
            'traffic_percent'=> $trafficPercent,
            'net_in_bps'     => round($netInBps, 2),
            'net_out_bps'    => round($netOutBps, 2),
            'net_in_rate'    => clicd_format_rate($netInBps),
            'net_out_rate'   => clicd_format_rate($netOutBps),
            'disk_read_bps'  => round($diskReadBps, 2),
            'disk_write_bps' => round($diskWriteBps, 2),
            'disk_read_rate' => clicd_format_rate($diskReadBps),
            'disk_write_rate'=> clicd_format_rate($diskWriteBps),
            'chart_time'     => date('H:i:s'),
            'usage'          => $usage,
        ],
        'debug'  => $debug,
    ];
}

function clicd_domain_status_from_container($container)
{
    if (!is_array($container)) {
        return 'Active';
    }

    if (!empty($container['policy_blocked'])) {
        return 'Suspended';
    }

    $status = strtolower(trim((string)($container['status'] ?? '')));
    if (in_array($status, ['suspended', 'blocked', 'policy_blocked', 'disabled'], true)) {
        return 'Suspended';
    }

    return 'Active';
}

function clicd_update_host_from_container($params, $container)
{
    $hostId = clicd_host_id($params);
    if ($hostId <= 0 || !is_array($container)) {
        return;
    }

    $update = [
        'domainstatus' => clicd_domain_status_from_container($container),
        'username'     => 'root',
        'dedicatedip'  => clicd_public_host($params, $container, true),
    ];

    $sshPort = clicd_container_ssh_port($container);
    if ($sshPort !== '') {
        $update['port'] = $sshPort;
    }

    $password = clicd_container_password($container);
    if ($password !== '') {
        $update['password'] = clicd_store_password($password);
    }

    try {
        Db::name('host')->where('id', $hostId)->update($update);
    } catch (\Exception $e) {
        clicd_debug('host update failed', $e->getMessage());
    }
}

function clicd_TestLink($params)
{
    $res = clicd_request($params, '/api/v1/dashboard', [], 'GET');
    return [
        'status' => 200,
        'data'   => [
            'server_status' => clicd_success($res) ? 1 : 0,
            'msg'           => clicd_success($res) ? '连接成功' : clicd_message($res, '连接失败'),
        ],
    ];
}

function clicd_CreateAccount($params)
{
    $exists = clicd_find_container($params);
    if (clicd_success($exists)) {
        return ['status' => 'error', 'msg' => '容器已存在，不能重复开通'];
    }

    $payload = clicd_container_payload($params);
    if (empty($payload['template_id'])) {
        return ['status' => 'error', 'msg' => '产品配置缺少 template_id'];
    }

    $res = clicd_request($params, '/api/v1/containers', $payload, 'POST', 120);
    if (!clicd_success($res)) {
        return ['status' => 'error', 'msg' => clicd_message($res, '开通失败')];
    }

    $hostId = clicd_host_id($params);
    if ($hostId > 0) {
        try {
            Db::name('host')->where('id', $hostId)->update([
                'domainstatus' => 'Active',
                'username'     => 'root',
                'dedicatedip'  => clicd_public_ipv4_from_routing($params) ?: clicd_public_host($params),
            ]);
        } catch (\Exception $e) {
            return ['status' => 'error', 'msg' => '开通成功但同步魔方数据库失败: ' . $e->getMessage()];
        }
    }

    $detail = clicd_find_container($params);
    if (clicd_success($detail) && isset($detail['data'])) {
        clicd_update_host_from_container($params, $detail['data']);
    }

    return ['status' => 'success', 'msg' => clicd_message($res, '开通成功')];
}

function clicd_TerminateAccount($params)
{
    $name = clicd_container_name($params);
    $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($name) . '/delete', [], 'DELETE', 60);
    return clicd_success($res)
        ? ['status' => 'success', 'msg' => clicd_message($res, '删除任务已提交')]
        : ['status' => 'error', 'msg' => clicd_message($res, '删除失败')];
}

function clicd_action($params, $action, $successMsg, $timeout = 60)
{
    $name = clicd_container_name($params);
    $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($name) . '/' . $action, [], 'POST', $timeout);
    return clicd_success($res)
        ? ['status' => 'success', 'msg' => clicd_message($res, $successMsg)]
        : ['status' => 'error', 'msg' => clicd_message($res, $successMsg . '失败')];
}

function clicd_On($params)
{
    return clicd_action($params, 'start', '开机任务已提交');
}

function clicd_Off($params)
{
    return clicd_action($params, 'stop', '关机任务已提交');
}

function clicd_Reboot($params)
{
    return clicd_action($params, 'restart', '重启任务已提交');
}

function clicd_SuspendAccount($params)
{
    return clicd_Off($params);
}

function clicd_UnsuspendAccount($params)
{
    return clicd_On($params);
}

function clicd_Status($params)
{
    $res = clicd_find_container($params);
    if (!clicd_success($res) || empty($res['data'])) {
        return ['status' => 'error', 'msg' => clicd_message($res, '查询失败')];
    }

    $status = strtolower($res['data']['status'] ?? '');
    if ($status === 'running') {
        return ['status' => 'success', 'data' => ['status' => 'on', 'des' => '运行中']];
    }
    if ($status === 'stopped') {
        return ['status' => 'success', 'data' => ['status' => 'off', 'des' => '已关机']];
    }
    return ['status' => 'success', 'data' => ['status' => 'unknown', 'des' => $status ?: '未知']];
}

function clicd_Sync($params)
{
    $res = clicd_find_container($params);
    if (!clicd_success($res) || empty($res['data'])) {
        return ['status' => 'error', 'msg' => clicd_message($res, '同步失败')];
    }
    clicd_update_host_from_container($params, $res['data']);
    return ['status' => 'success', 'msg' => '同步成功'];
}

function clicd_Reinstall($params)
{
    $templateId = $params['reinstall_os'] ?? '';
    if ($templateId === '') {
        $templateId = ($params['configoptions']['template_id'] ?? '');
    }
    if ($templateId === '') {
        return ['status' => 'error', 'msg' => '缺少重装系统模板 ID'];
    }

    $name = clicd_container_name($params);
    $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($name) . '/reinstall', ['template_id' => $templateId], 'POST', 60);
    if (clicd_success($res)) {
        $detail = clicd_find_container($params);
        if (clicd_success($detail) && isset($detail['data'])) {
            clicd_update_host_from_container($params, $detail['data']);
        } elseif (isset($res['data']) && is_array($res['data'])) {
            clicd_update_host_from_container($params, $res['data']);
        }
    }
    return clicd_success($res)
        ? ['status' => 'success', 'msg' => clicd_message($res, '重装任务已提交')]
        : ['status' => 'error', 'msg' => clicd_message($res, '重装失败')];
}

function clicd_CrackPassword($params, $new_pass)
{
    $name = clicd_container_name($params);
    $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($name) . '/reset-password', ['password' => $new_pass], 'POST', 60);
    if (!clicd_success($res)) {
        return ['status' => 'error', 'msg' => clicd_message($res, '重置密码失败')];
    }

    $password = $res['data']['ssh_password'] ?? $res['data']['password'] ?? $new_pass;
    $hostId = clicd_host_id($params);
    if ($hostId > 0) {
        try {
            Db::name('host')->where('id', $hostId)->update(['password' => clicd_store_password($password)]);
            $detail = clicd_find_container($params);
            if (clicd_success($detail) && isset($detail['data'])) {
                clicd_update_host_from_container($params, $detail['data']);
            }
        } catch (\Exception $e) {
            return ['status' => 'error', 'msg' => '密码重置成功但同步魔方数据库失败: ' . $e->getMessage()];
        }
    }

    return ['status' => 'success', 'msg' => clicd_message($res, '密码重置成功')];
}

function clicd_TrafficReset($params)
{
    $name = clicd_container_name($params);
    $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($name) . '/traffic-reset', [], 'POST', 30);
    return clicd_success($res)
        ? ['status' => 'success', 'msg' => clicd_message($res, '流量已重置')]
        : ['status' => 'error', 'msg' => clicd_message($res, '流量重置失败')];
}

function clicd_randomPort($params)
{
    $container = [];
    $containerId = clicd_container_api_id($params, $container);
    $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($containerId) . '/random-port', [], 'GET', 30);
    if (!clicd_success($res)) {
        return ['status' => 'error', 'msg' => clicd_message($res, '获取随机端口失败')];
    }

    $port = $res['data']['port'] ?? '';
    return ['status' => 200, 'msg' => $port ? '随机端口: ' . $port : '随机端口获取成功', 'data' => ['port' => $port]];
}

function clicd_addNat($params)
{
    $payload = clicd_nat_payload_from_post();
    if (isset($payload['error'])) {
        return ['status' => 'error', 'msg' => $payload['error']];
    }

    $container = [];
    $containerId = clicd_container_api_id($params, $container);
    $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($containerId) . '/port-mappings', $payload, 'POST', 30);
    return clicd_success($res)
        ? ['status' => 200, 'msg' => clicd_message($res, '端口映射添加成功'), 'data' => ['port_mappings' => clicd_normalize_port_mappings($res['data'] ?? [])]]
        : ['status' => 'error', 'msg' => clicd_message($res, '端口映射添加失败')];
}

function clicd_updateNat($params)
{
    $index = clicd_request_value('index', '');
    if ($index === '' || !is_numeric($index) || (int)$index < 0) {
        return ['status' => 'error', 'msg' => '端口映射索引错误'];
    }

    $payload = clicd_nat_payload_from_post();
    if (isset($payload['error'])) {
        return ['status' => 'error', 'msg' => $payload['error']];
    }

    $container = [];
    $containerId = clicd_container_api_id($params, $container);
    $endpoint = '/api/v1/containers/' . rawurlencode($containerId) . '/port-mappings/' . rawurlencode((string)(int)$index);
    $res = clicd_request($params, $endpoint, $payload, 'PUT', 30);
    return clicd_success($res)
        ? ['status' => 200, 'msg' => clicd_message($res, '端口映射更新成功'), 'data' => ['port_mappings' => clicd_normalize_port_mappings($res['data'] ?? [])]]
        : ['status' => 'error', 'msg' => clicd_message($res, '端口映射更新失败')];
}

function clicd_deleteNat($params)
{
    $index = clicd_request_value('index', '');
    if ($index === '' || !is_numeric($index) || (int)$index < 0) {
        return ['status' => 'error', 'msg' => '端口映射索引错误'];
    }

    $container = [];
    $containerId = clicd_container_api_id($params, $container);
    $endpoint = '/api/v1/containers/' . rawurlencode($containerId) . '/port-mappings/' . rawurlencode((string)(int)$index);
    $res = clicd_request($params, $endpoint, [], 'DELETE', 30);
    return clicd_success($res)
        ? ['status' => 200, 'msg' => clicd_message($res, '端口映射删除成功'), 'data' => ['port_mappings' => clicd_normalize_port_mappings($res['data'] ?? [])]]
        : ['status' => 'error', 'msg' => clicd_message($res, '端口映射删除失败')];
}

function clicd_natList($params)
{
    $res = clicd_find_container($params);
    if (!clicd_success($res) || empty($res['data']) || !is_array($res['data'])) {
        return ['status' => 'error', 'msg' => clicd_message($res, '获取 NAT 列表失败')];
    }

    return [
        'status' => 200,
        'msg'    => '获取成功',
        'data'   => [
            'port_mappings' => clicd_normalize_port_mappings(clicd_port_mappings_from_container($res['data'])),
            'debug' => [
                clicd_debug_entry('NatList', [
                    'container' => [
                        'id'   => $res['data']['id'] ?? null,
                        'uuid' => $res['data']['uuid'] ?? null,
                        'name' => $res['data']['name'] ?? null,
                    ],
                    'http' => $res['_http_code'] ?? null,
                ]),
            ],
        ],
    ];
}

function clicd_infoData($params)
{
    $data = clicd_info_ajax($params);
    if (($data['status'] ?? '') !== 'success') {
        return [
            'status' => 200,
            'msg'    => $data['msg'] ?? '流量统计暂不可用',
            'data'   => [
                'cpu_percent'    => 0,
                'cpu_detail'     => '-',
                'mem_percent'    => 0,
                'mem_detail'     => '-',
                'load_percent'   => 0,
                'load_detail'    => '-',
                'disk_percent'   => 0,
                'disk_detail'    => '-',
                'traffic_used'   => '-',
                'traffic_limit'  => '-',
                'traffic_in_gb'  => '-',
                'traffic_out_gb' => '-',
                'traffic_used_text' => '-',
                'traffic_limit_text'=> '-',
                'traffic_in_text'   => '-',
                'traffic_out_text'  => '-',
                'traffic_percent'=> 0,
                'net_in_bps'     => 0,
                'net_out_bps'    => 0,
                'net_in_rate'    => '0 B/s',
                'net_out_rate'   => '0 B/s',
                'disk_read_bps'  => 0,
                'disk_write_bps' => 0,
                'disk_read_rate' => '0 B/s',
                'disk_write_rate'=> '0 B/s',
                'chart_time'     => date('H:i:s'),
                'debug'          => $data['debug'] ?? [],
            ],
        ];
    }

    $data['data']['debug'] = $data['debug'] ?? [];
    return ['status' => 200, 'msg' => '获取成功', 'data' => $data['data']];
}

function clicd_ChangePackage($params)
{
    $options = $params['configoptions'] ?? [];
    $name = clicd_container_name($params);

    $resource = [
        'vcpu'             => clicd_float_option($options, 'vcpu', 0),
        'ram_mb'           => clicd_int_option($options, 'ram_mb', 0),
        'io_speed_mbps'    => clicd_int_option($options, 'io_speed_mbps', 0),
        'network_bw_mbps'  => clicd_int_option($options, 'network_bw_mbps', 0),
    ];
    $resource = array_filter($resource, function ($value) {
        return $value !== 0 && $value !== 0.0;
    });

    if (!empty($resource)) {
        $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($name) . '/resource-limit', $resource, 'PUT', 60);
        if (!clicd_success($res)) {
            return ['status' => 'error', 'msg' => clicd_message($res, '资源限制调整失败')];
        }
    }

    $traffic = [
        'traffic_mode'       => $options['traffic_mode'] ?? 'total',
        'monthly_traffic_gb' => clicd_int_option($options, 'monthly_traffic_gb', 0),
        'traffic_in_gb'      => clicd_int_option($options, 'traffic_in_gb', 0),
        'traffic_out_gb'     => clicd_int_option($options, 'traffic_out_gb', 0),
    ];
    $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($name) . '/traffic-limit', $traffic, 'PUT', 30);
    if (!clicd_success($res)) {
        return ['status' => 'error', 'msg' => clicd_message($res, '流量限制调整失败')];
    }

    $expiresAt = clicd_expiry_from_params($params);
    if ($expiresAt !== '') {
        $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($name) . '/expiry', ['expires_at' => $expiresAt], 'PUT', 30);
        if (!clicd_success($res)) {
            return ['status' => 'error', 'msg' => clicd_message($res, '到期时间同步失败')];
        }
    }

    return ['status' => 'success', 'msg' => '配置变更成功'];
}

function clicd_Renew($params)
{
    $expiresAt = clicd_expiry_from_params($params);
    if ($expiresAt === '') {
        return ['status' => 'success', 'msg' => '未启用到期时间同步'];
    }
    $name = clicd_container_name($params);
    $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($name) . '/expiry', ['expires_at' => $expiresAt], 'PUT', 30);
    return clicd_success($res)
        ? ['status' => 'success', 'msg' => '续费到期时间同步成功']
        : ['status' => 'error', 'msg' => clicd_message($res, '续费同步失败')];
}

function clicd_AdminButton($params)
{
    if (empty($params['domain'])) {
        return [];
    }
    return [
        'Sync'         => '同步状态',
        'TrafficReset' => '重置流量',
    ];
}

function clicd_ClientButton($params)
{
    if (empty($params['domain'])) {
        return [];
    }
    return [
        'webssh' => [
            'place' => 'console',
            'name'  => 'WebSSH',
        ],
    ];
}

function clicd_webssh($params)
{
    $container = [];
    $containerName = clicd_container_name($params);
    clicd_container_api_id($params, $container);
    if (!empty($container['name'])) {
        $containerName = (string)$container['name'];
    }

    $res = clicd_request($params, '/api/v1/ssh-ticket', ['container_name' => $containerName], 'POST', 30);
    if (!clicd_success($res)) {
        return ['status' => 'error', 'msg' => clicd_message($res, 'WebSSH ticket create failed')];
    }

    $ticket = $res['data']['ticket'] ?? '';
    if ($ticket === '') {
        return ['status' => 'error', 'msg' => 'WebSSH ticket is empty'];
    }

    $url = clicd_webssh_url($params, $ticket, $containerName);
    $jsUrl = json_encode($url, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);

    return [
        'status' => 'success',
        'msg'    => "WebSSH started<script type='text/javascript'>window.open({$jsUrl}, '_blank');</script>",
    ];
}

function clicd_firewallList($params)
{
    $container = [];
    $containerId = clicd_container_api_id($params, $container);
    $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($containerId) . '/firewall', [], 'GET', 30);
    if (!clicd_success($res) || empty($res['data'])) {
        return ['status' => 'error', 'msg' => clicd_message($res, '获取防火墙设置失败')];
    }

    return [
        'status' => 200,
        'msg'    => '获取成功',
        'data'   => $res['data'],
    ];
}

function clicd_firewallUpdate($params)
{
    $input = clicd_json_input();
    $enabled = clicd_param_value($input, 'enabled', 'true');
    $enabled = filter_var($enabled, FILTER_VALIDATE_BOOLEAN);
    $defaultAction = strtoupper(trim((string)clicd_param_value($input, 'default_action', '')));
    $rules = clicd_param_value($input, 'rules', '[]');

    if (is_string($rules)) {
        $decodedRules = json_decode($rules, true);
        if (is_array($decodedRules)) {
            $rules = $decodedRules;
        }
    }
    if (!is_array($rules)) {
        $rules = [];
    }

    $payload = [
        'enabled' => $enabled,
        'rules'   => $rules,
    ];
    if (in_array($defaultAction, ['ACCEPT', 'DROP'], true)) {
        $payload['default_action'] = $defaultAction;
    }

    $container = [];
    $containerId = clicd_container_api_id($params, $container);
    $res = clicd_request($params, '/api/v1/containers/' . rawurlencode($containerId) . '/firewall', $payload, 'PUT', 30);
    if (!clicd_success($res)) {
        return ['status' => 'error', 'msg' => clicd_message($res, '更新防火墙设置失败')];
    }

    // GET after PUT to confirm the actual state after CLICD processes it
    $getRes = clicd_request($params, '/api/v1/containers/' . rawurlencode($containerId) . '/firewall', [], 'GET', 30);
    $actualData = [];
    if (clicd_success($getRes) && !empty($getRes['data']) && is_array($getRes['data'])) {
        $actualData = $getRes['data'];
    }

    return [
        'status' => 200,
        'msg'    => clicd_message($res, '防火墙设置已更新'),
        'data'   => $actualData,
    ];
}

function clicd_firewall_ajax($params)
{
    $input = clicd_json_input();
    $action = strtolower(trim((string)clicd_param_value($input, 'action', '')));
    $debug = [clicd_debug_entry('Firewall ajax received', [
        'action' => $action,
        'input'  => $input,
        'query'  => $_GET,
    ])];

    $container = [];
    $containerId = clicd_container_api_id($params, $container);
    $debug[] = clicd_debug_entry('Container resolved', [
        'container_id' => $containerId,
        'container'    => [
            'id'   => $container['id'] ?? null,
            'uuid' => $container['uuid'] ?? null,
            'name' => $container['name'] ?? null,
        ],
    ]);

    if (!in_array($action, ['list', 'update'], true)) {
        return ['status' => 'error', 'msg' => '未知防火墙操作', 'debug' => $debug];
    }

    if ($action === 'list') {
        $call = clicd_request_debug($params, '/api/v1/containers/' . rawurlencode($containerId) . '/firewall', [], 'GET', 30);
        $debug[] = $call['debug'];
        $res = $call['response'];
        if (!clicd_success($res) || empty($res['data'])) {
            return ['status' => 'error', 'msg' => clicd_message($res, '获取防火墙设置失败'), 'debug' => $debug];
        }
        return [
            'status' => 'success',
            'msg'    => '获取成功',
            'data'   => $res['data'],
            'debug'  => $debug,
        ];
    }

    // update
    $enabled = clicd_param_value($input, 'enabled', 'true');
    $enabled = filter_var($enabled, FILTER_VALIDATE_BOOLEAN);
    $defaultAction = strtoupper(trim((string)clicd_param_value($input, 'default_action', '')));
    $rules = clicd_param_value($input, 'rules', '[]');

    if (is_string($rules)) {
        $decodedRules = json_decode($rules, true);
        if (is_array($decodedRules)) {
            $rules = $decodedRules;
        }
    }
    if (!is_array($rules)) {
        $rules = [];
    }

    $payload = [
        'enabled' => $enabled,
        'rules'   => $rules,
    ];
    if (in_array($defaultAction, ['ACCEPT', 'DROP'], true)) {
        $payload['default_action'] = $defaultAction;
    }

    $call = clicd_request_debug($params, '/api/v1/containers/' . rawurlencode($containerId) . '/firewall', $payload, 'PUT', 30);
    $debug[] = $call['debug'];
    $res = $call['response'];

    if (!clicd_success($res)) {
        return ['status' => 'error', 'msg' => clicd_message($res, '更新防火墙设置失败'), 'debug' => $debug];
    }

    return [
        'status' => 'success',
        'msg'    => clicd_message($res, '防火墙设置已更新'),
        'data'   => $res['data'] ?? [],
        'debug'  => $debug,
    ];
}

function clicd_AllowFunction()
{
    return [
        'client' => ['TrafficReset', 'randomPort', 'addNat', 'updateNat', 'deleteNat', 'natList', 'infoData', 'webssh', 'firewallList', 'firewallUpdate'],
        'admin'  => ['TrafficReset', 'randomPort', 'addNat', 'updateNat', 'deleteNat', 'natList', 'infoData', 'webssh', 'firewallList', 'firewallUpdate'],
    ];
}

function clicd_ClientArea($params)
{
    return [
        'info'     => ['name' => '实例信息'],
        'nat'      => ['name' => 'NAT转发'],
        'firewall' => ['name' => '防火墙'],
    ];
}

function clicd_ClientAreaOutput($params, $key)
{
    $func = strtolower(trim((string)clicd_request_value('func', '')));
    if ($func === 'natajax') {
        clicd_json_response(clicd_nat_ajax($params));
    }
    if ($func === 'infoajax') {
        clicd_json_response(clicd_info_ajax($params));
    }
    if ($func === 'firewallajax') {
        clicd_json_response(clicd_firewall_ajax($params));
    }

    if (!in_array($key, ['info', 'nat', 'firewall'], true)) {
        return '';
    }

    $res = clicd_find_container($params);
    if (!clicd_success($res) || empty($res['data'])) {
        return '获取实例信息失败: ' . clicd_message($res, '未知错误');
    }

    $c = $res['data'];
    $publicHost = clicd_public_host($params, $c, true);

    if ($key === 'nat') {
        $operation = clicd_handle_nat_post($params);
        $operationMsg = $operation['message'] ?? '';
        $postMappings = $operation['mappings'] ?? null;
        if ($operationMsg !== '') {
            $res = clicd_find_container($params);
            $c = clicd_success($res) && !empty($res['data']) && is_array($res['data']) ? $res['data'] : $c;
        }
        $mappings = $postMappings !== null ? $postMappings : clicd_port_mappings_from_container($c);

        return [
            'template' => 'templates/nat.html',
            'vars'     => [
                'container'     => $c,
                'container_name'=> $c['name'] ?? clicd_container_name($params),
                'ssh_port'      => $c['ssh_port'] ?? '',
                'server_ip'     => $publicHost,
                'nat_host'      => $publicHost,
                'operation_msg' => $operationMsg,
                'service_id'    => clicd_request_value('id', $params['hostid'] ?? ''),
                'area_key'      => 'nat',
                'port_mappings' => clicd_normalize_port_mappings($mappings),
            ],
        ];
    }

    if ($key === 'firewall') {
        return [
            'template' => 'templates/firewall.html',
            'vars'     => [
                'container'      => $c,
                'container_name' => $c['name'] ?? clicd_container_name($params),
                'server_ip'      => $publicHost,
                'service_id'     => clicd_request_value('id', $params['hostid'] ?? ''),
                'area_key'       => 'firewall',
            ],
        ];
    }

    $initialRxBytes = (int)($c['traffic_used_rx'] ?? $c['rx_bytes'] ?? 0);
    $initialTxBytes = (int)($c['traffic_used_tx'] ?? $c['tx_bytes'] ?? 0);
    $initialTrafficUsed = ($initialRxBytes || $initialTxBytes) ? round(($initialRxBytes + $initialTxBytes) / 1073741824, 2) : '-';
    $initialTrafficIn = $initialRxBytes ? round($initialRxBytes / 1073741824, 2) : '-';
    $initialTrafficOut = $initialTxBytes ? round($initialTxBytes / 1073741824, 2) : '-';
    $initialTrafficLimit = isset($c['monthly_traffic_gb']) && $c['monthly_traffic_gb'] !== '' ? $c['monthly_traffic_gb'] : '-';
    $initialTrafficUsedText = ($initialRxBytes || $initialTxBytes) ? clicd_format_bytes($initialRxBytes + $initialTxBytes) : '-';
    $initialTrafficInText = $initialRxBytes ? clicd_format_bytes($initialRxBytes) : '-';
    $initialTrafficOutText = $initialTxBytes ? clicd_format_bytes($initialTxBytes) : '-';
    $initialTrafficLimitText = is_numeric($initialTrafficLimit) ? round((float)$initialTrafficLimit, 2) . ' GB' : '-';
    $options = $params['configoptions'] ?? [];

    return [
        'template' => 'templates/info.html',
        'vars'     => [
            'container'      => $c,
            'status_text'    => (($c['status'] ?? '') === 'running') ? '运行中' : '已关机',
            'server_ip'      => $publicHost,
            'ssh_host'       => $publicHost,
            'ssh_port'       => $c['ssh_port'] ?? '',
            'ssh_password'   => $c['ssh_password'] ?? '',
            'ipv4'           => $c['ip'] ?? '',
            'ipv6'           => $c['ipv6'] ?? '',
            'vcpu'           => $c['vcpu'] ?? ($options['vcpu'] ?? ''),
            'ram_mb'         => $c['ram_mb'] ?? ($options['ram_mb'] ?? ''),
            'disk_gb'        => $c['disk_gb'] ?? ($options['disk_gb'] ?? ''),
            'bandwidth'      => $c['network_bw_mbps'] ?? ($options['network_bw_mbps'] ?? ''),
            'traffic_used'   => $initialTrafficUsed,
            'traffic_limit'  => $initialTrafficLimit,
            'traffic_in_gb'  => $initialTrafficIn,
            'traffic_out_gb' => $initialTrafficOut,
            'traffic_used_text' => $initialTrafficUsedText,
            'traffic_limit_text'=> $initialTrafficLimitText,
            'traffic_in_text'   => $initialTrafficInText,
            'traffic_out_text'  => $initialTrafficOutText,
            'expires_at'     => $c['expires_at'] ?? '',
            'service_id'     => clicd_request_value('id', $params['hostid'] ?? ''),
            'area_key'       => 'info',
        ],
    ];
}
