
# mihomo-proxy-pool

基于 [mihomo](https://github.com/MetaCubeX/mihomo) 的本地代理池服务，当前使用的是 Go 版本实现。

服务会把订阅或单个代理导入本地代理池，给每个节点分配本地端口，定时做健康检查，并通过 HTTP API 提供查询和管理能力。

## 功能概览

- 导入单个代理链接或订阅链接
- 为每个代理分配本地监听端口
- 每 10 分钟执行一次健康检查，连续失败会自动清理
- 每 24 小时刷新一次订阅
- 通过 REST API 获取、添加、删除代理和订阅

## 运行依赖

- Go 1.23+
- Redis，默认连接 `127.0.0.1:6379/0`

当前代码里的默认监听地址是 `0.0.0.0:9999`，Redis 地址也写死在代码中。

## 本地启动

先确保本地 Redis 已启动，然后执行：

```bash
go run .
```

或先编译再运行：

```bash
go build -o mihomo-proxy-pool .
./mihomo-proxy-pool
```

启动后服务会监听：

```text
http://127.0.0.1:9999
```

## Docker 构建

仓库提供了 Go 版本的 `Dockerfile`，可以直接构建镜像：

```bash
docker build -t mihomo-proxy-pool .
```

`docker-compose.yml` 目前偏向现有部署环境，依赖仓库外的镜像地址。

## API 示例

健康检查：

```bash
curl http://127.0.0.1:9999/
```

获取随机代理：

```bash
curl http://127.0.0.1:9999/get
```

获取全部代理：

```bash
curl "http://127.0.0.1:9999/all?sort=delay"
```

添加订阅：

```bash
curl -X POST http://127.0.0.1:9999/sub/add \
  -H 'Content-Type: application/json' \
  -d '{
    "sub_name": "demo",
    "sub_url": "https://example.com/sub.yaml",
    "update": true
  }'
```

查看全部订阅：

```bash
curl http://127.0.0.1:9999/sub/all
```

删除订阅：

```bash
curl -X DELETE http://127.0.0.1:9999/sub/del \
  -H 'Content-Type: application/json' \
  -d '{
    "sub_name": "demo"
  }'
```

删除代理：

```bash
curl -X DELETE http://127.0.0.1:9999/del \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "1.2.3.4:443"
  }'
```

## 项目结构

- `main.go`: 服务入口
- `proxypool/`: 代理池和订阅管理
- `server/`: HTTP API
- `db/`: Redis 封装
- `ipinfo/`: 出口 IP 信息补充
