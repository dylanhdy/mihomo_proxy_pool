# Proxy State And Port Consistency

This document describes the current proxy state model, the latest Redis/runtime consistency check, and the known behavior of the `83df705` deployment.

## Current Runtime State

Latest check result:

- Redis `proxy:*` records: `374`.
- Valid proxy JSON records: `374`.
- Records with missing or invalid `local_port`: `0`.
- Unique Redis `local_port` values: `374`.
- Duplicate Redis `local_port` values: `0`.
- TCP listeners owned by `mihomo-proxy-pool`: `370`.
- Redis records without a matching TCP listener: `4`.
- TCP listeners owned by `mihomo-proxy-pool` without a Redis record: `0`.

Redis currently has no duplicate local ports. The remaining inconsistency is that four Redis proxy records have no matching TCP listener in the running process:

- `40124`
- `40236`
- `40330`
- `40370`

This means local port allocation state is clean from a duplication perspective, but a small number of proxies may be logically present in Redis while not actually serving a local TCP listener.

New local port allocation is configured to start at `61000`. This avoids the default Linux ephemeral port range observed on the host, `32768-60999`, and reduces collisions with short-lived outbound connections. Existing Redis records keep their stored `local_port` values until Redis is rebuilt or the records are migrated.

## State Model

The proxy pool maintains several layers of state:

- Redis is the persistent source of truth for subscriptions and proxy records.
- `localPortMaps` is an in-memory allocation map: `local_port -> proxy name`.
- `listeners` is an in-memory inbound listener config map: `in_<local_port> -> listener`.
- `cproxies` is an in-memory outbound proxy map: `proxy name -> parsed mihomo proxy`.
- mihomo also has its own internal inbound listener state.

Only Redis survives a process restart. OS sockets, `localPortMaps`, `listeners`, `cproxies`, and mihomo internal listener state are rebuilt from Redis.

## Restart Recovery

On startup:

1. `main()` calls `proxypool.InitProxyPool()`.
2. `InitProxyPool()` connects to Redis hash `mihomo_proxy_pool`.
3. It scans all `proxy:*` records.
4. For each record, it unmarshals `Proxy`.
5. It parses `proxy.Config` into a mihomo outbound proxy and inserts it into `cproxies`.
6. It creates an inbound listener from `proxy.LocalPort` and inserts it into `listeners`.
7. It sets `localPortMaps[proxy.LocalPort] = proxyName`.
8. It calls `tunnel.UpdateProxies(cproxies, nil)`.
9. It calls `startListen(listeners, true)`.

Important behavior:

- A restart clears process-local dirty OS sockets.
- A restart does not remove invalid or stale Redis records.
- If Redis contains duplicate `local_port` values, startup does not reject them.
- If a Redis record cannot be parsed into a proxy/listener, it remains in Redis but does not enter memory.
- If a listener fails to start during recovery, the Redis record remains.

## How `localPortMaps` Is Maintained

`localPortMaps` is used as the local port allocation guard.

It is populated and updated in these paths:

- Startup: each successfully parsed Redis proxy sets `localPortMaps[proxy.LocalPort] = proxyName`.
- Add proxy: after the proxy/listener path is created, `AddProxy()` sets `localPortMaps[localPort] = name`.
- Delete proxy: `deleteProxyLocked()` deletes `localPortMaps[proxy.LocalPort]`.

With a clean Redis dataset, one running proxy-pool process, and normal API paths, `83df705` should not create duplicate local ports because `AddProxy()` holds the global mutex while selecting and committing a port.

For new allocations, `getLocalPortLocked()` scans from `61000` upward. This only affects newly added proxies; startup recovery still uses the `local_port` stored in Redis.

## Listen Failure Behavior In `83df705`

In the current `83df705` deployment, `startListen()` follows upstream mihomo behavior and does not return listener startup errors to business code.

As a result:

- `AddProxy()` cannot reliably know whether mihomo actually listened on the local port.
- A proxy can be written to Redis even if its local listener failed to start.
- This can create Redis records where `local_port` is allocated logically but no TCP listener exists in the process.

This failure mode does not necessarily cause duplicate port allocation as long as `localPortMaps` still contains the port. It causes an unavailable local proxy endpoint.

## Duplicate Port Risk

Current Redis data has no duplicate ports. Remaining ways duplicate ports could appear are:

- Running multiple proxy-pool instances against the same Redis.
- Manually modifying Redis.
- Starting from Redis data that already contains duplicate `local_port` values.
- Deleting one proxy when Redis already has duplicate ports, because `deleteProxyLocked()` deletes `localPortMaps` by port without checking the current owner.

## Operational Checks

Use this kind of check to compare Redis ports and runtime TCP listeners:

```bash
python3 - <<'PY'
import json, re, subprocess, collections

raw = subprocess.check_output([
    "docker", "exec", "redis", "redis-cli", "-n", "0", "--raw",
    "HSCAN", "mihomo_proxy_pool", "0", "MATCH", "proxy:*", "COUNT", "100000",
], text=True)

lines = raw.splitlines()
fields = lines[1:]
ports = collections.defaultdict(list)
bad_json = 0
bad_port = 0

for i in range(0, len(fields) - 1, 2):
    key, val = fields[i], fields[i + 1]
    try:
        obj = json.loads(val)
    except Exception:
        bad_json += 1
        continue
    port = obj.get("local_port")
    if not isinstance(port, int):
        bad_port += 1
        continue
    ports[port].append(key)

ss_tcp = subprocess.check_output(["ss", "-ltnp"], text=True)
os_tcp = set()
for line in ss_tcp.splitlines():
    cols = line.split()
    if len(cols) < 4:
        continue
    m = re.search(r":(\d+)$", cols[3])
    if m and 40001 <= int(m.group(1)) <= 65535 and "mihomo-proxy-po" in line:
        os_tcp.add(int(m.group(1)))

db_ports = set(ports)
dups = {p: keys for p, keys in ports.items() if len(keys) > 1}

print("proxy_records", sum(len(v) for v in ports.values()) + bad_json + bad_port)
print("bad_json", bad_json)
print("missing_or_bad_local_port", bad_port)
print("unique_db_ports", len(db_ports))
print("duplicate_local_ports", len(dups))
print("os_tcp_ports_by_process", len(os_tcp))
print("db_not_tcp", sorted(db_ports - os_tcp))
print("tcp_not_db", sorted(os_tcp - db_ports))
PY
```
