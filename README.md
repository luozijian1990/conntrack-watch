# eBPF Container Egress Watch

在连接发起侧，查清哪个容器 IP 访问了哪个外部服务。

节点级 TCP 出站观测工具，通过 `sock:inet_sock_set_state` tracepoint 记录来源、目标、建连结果和耗时。无需修改应用，支持 JSON 日志、Prometheus 指标和 Grafana 看板。

[快速开始](#快速开始) · [配置](#配置) · [连接日志](#连接日志) · [监控](#监控) · [Kubernetes 部署](#kubernetes-部署) · [能力边界](#能力边界) · [开发与验证](#开发与验证)

本分支 `ebpf` 的项目入口就在仓库根目录；原 conntrack 实现保留在 `main` 分支。

## 用途与工作原理

容器访问外部 Nginx、数据库等服务时，节点 SNAT 可能让服务端只看到 Node IP。本工具部署在容器所在节点，记录 socket 视角的来源 IP 和目标，帮助从连接日志中定位发起方。

```text
容器 10.244.1.23:45678 → 外部服务 172.16.10.50:443
          │
          ├── eBPF 记录来源、目标、建连结果与耗时
          └── 节点 SNAT → 服务端看到 Node IP

CLOSE → SYN_SENT：保存来源 IP、netns、发起进程身份
          ├── ESTABLISHED：成功、源端口、耗时
          └── CLOSE：未建立即关闭、耗时
                  ↓ ring buffer
          Go 解码与 CIDR 过滤 → JSON 日志 / Prometheus
```

按 socket 关联尝试与结果，后续状态变化沿用发起时保存的进程身份；网络命名空间取自 socket。结果输出后删除 pending 记录。

| 能力 | 输出 |
| --- | --- |
| 来源定位 | 源/目标 IP 与端口、netns、PID、comm、cgroup ID |
| 建连观测 | 尝试、成功、未建立即关闭，以及结果事件的耗时 |
| 范围过滤 | 目标端口、来源 CIDR、目标 CIDR、目标排除范围 |
| 日志采集 | 逐行 JSON，输出到 stdout 或轮转文件 |
| 运行监控 | 连接趋势、成功建连延迟、采集错误、就绪状态 |

## 快速开始

### 1. 准备环境并编译

| 项目 | 要求 |
| --- | --- |
| 运行节点 | Linux，amd64 或 arm64，小端；目标内核 5.15+ |
| 内核能力 | BTF、BPF、tracepoint、ring buffer；可读取 `/sys/kernel/btf/vmlinux` |
| 编译工具 | Go 1.21+、带 BPF 后端的 Clang、make |
| 运行权限 | BPF / tracepoint 权限；以下示例使用 root |

以下命令均在仓库根目录执行：

```sh
make build GOARCH=amd64  # ARM64 节点使用 GOARCH=arm64
```

产物为 `build/egress-watch` 和 `build/egress.bpf.o`，运行时需要同时保留。`make build` 生成 Linux 可执行文件；macOS 可运行 Go 单元测试和配置校验，采集需在 Linux 上执行。没有本地 Linux 编译环境时，可使用下文的 Docker 构建方式。

> [!NOTE]
> 已有真实加载与容器流量验证来自 Docker Linux 6.12 / arm64。最低目标内核、amd64 实际加载和目标 Kubernetes/CNI 仍需验收，详见 [VALIDATION.md](VALIDATION.md)。

### 2. 配置观测范围

编辑 [config.yaml](config.yaml)，替换真实的 Pod 和 Service 网段：

```yaml
ports: [80, 443]
source_cidrs: ["10.244.0.0/16"]
destination_cidrs: []
exclude_cidrs: ["10.244.0.0/16", "10.96.0.0/12", "127.0.0.0/8", "::1/128"]
```

> [!IMPORTANT]
> `source_cidrs` 必须非空。示例网段不会自动适配集群；`exclude_cidrs` 排除的是目标地址。外部服务也可以使用公司内网 IP，无需排除所有私网地址。

### 3. 启动与检查

在 Linux 节点执行：

```sh
./build/egress-watch -config config.yaml -check-config

# 仅在节点尚未挂载 tracefs 时执行
sudo mount -t tracefs tracefs /sys/kernel/tracing
sudo ./build/egress-watch -config config.yaml
```

从匹配来源范围的容器发起目标端口连接，并检查：

```sh
curl -fsS http://127.0.0.1:9359/readyz
curl -fsS http://127.0.0.1:9359/metrics
```

默认连接日志写入 stdout，运行诊断以 JSON 写入 stderr。默认只输出建连结果；设置 `log_attempts: true` 可额外输出尝试日志。

## 配置

配置严格校验未知字段、CIDR、端口和轮转参数，修改后可用 `-check-config` 校验而不挂载 eBPF。

| 配置项 | 默认值 / 说明 |
| --- | --- |
| `ports` | `[80, 443]`；不可为空、重复或包含 0 |
| `source_cidrs` | 必填；来源 Pod / 容器网段，支持 IPv4 和 IPv6 |
| `destination_cidrs` | `[]`；允许的目标范围，空表示不限 |
| `exclude_cidrs` | `[]`；排除目标，优先于允许范围；示例文件已填写示例网段 |
| `object_path` | `build/egress.bpf.o`；相对路径以工作目录为基准 |
| `host_netns_path` | `/proc/1/ns/net`；须指向真实宿主机网络命名空间 |
| `listen_addr` | `:9359`；HTTP 监听地址 |
| `node` | 依次使用配置值、`NODE_NAME`、hostname |
| `log_attempts` | `false`；仅影响尝试日志，不影响尝试计数 |
| `log.path` | 空表示 stdout；填写路径则使用轮转文件 |
| `log.max_size_mb` | `100`；单文件大小上限 |
| `log.max_backups` / `log.max_age_days` | `10` / `7`；备份数与保留天数，旧日志压缩；轮转参数均须为正数 |

内核按目标端口过滤并排除 host netns，Go 再过滤 CIDR。IPv4-mapped IPv6 地址按 IPv4 CIDR 匹配，日志 `family` 保留 socket 的 `ipv6` 类型。

## 连接日志

以下为展开后的示例，实际每条事件占一行：

```json
{
  "ts": "2026-10-08T08:00:00Z",
  "type": "egress_connection",
  "source": "ebpf_tracepoint",
  "event": "established",
  "node": "worker-1",
  "protocol": "tcp",
  "family": "ipv4",
  "src_ip": "10.244.1.23",
  "src_port": 45678,
  "src_port_known": true,
  "dst_ip": "172.16.10.50",
  "dst_port": 443,
  "netns": 4026533000,
  "pid": 1234,
  "cgroup_id": "98765",
  "comm": "curl",
  "started_monotonic_ns": 123456000000,
  "connect_duration_ms": 3.2
}
```

| `event` | 含义 |
| --- | --- |
| `attempt` | 主动进入 `SYN_SENT`；源端口可能尚未分配 |
| `established` | 关联到 socket 进入 `ESTABLISHED`，补齐源端口 |
| `closed_before_established` | 建连阶段后关闭；可能是拒绝、超时或应用取消，不提供具体失败原因 |

`src_port_known=false` 与 `src_port=0` 表示端口尚未确定。结果事件的 `connect_duration_ms` 从内核尝试时间计算，不包含用户态排队时间；`ts` 从内核事件时间换算为 UTC。

`pid` 是宿主机 TGID，`comm` 最长 15 字节，`cgroup_id` 是内核标识。`started_monotonic_ns` 可结合节点、netns、IP 和进程辅助关联事件，不能作为跨节点、跨重启的全局唯一 ID。

Filebeat 可按 `type=egress_connection` 筛选连接日志，在 Elasticsearch 中按时间、`src_ip`、`dst_ip`、`dst_port` 和 `event` 查询。项目提供日志输出，不包含 Filebeat / Elasticsearch 部署配置。

## 监控

| HTTP 端点 | 用途 |
| --- | --- |
| `/metrics` | Prometheus 抓取 |
| `/healthz` | HTTP 服务存活检查 |
| `/readyz` | 采集器挂载并运行时返回 200，否则返回 503 |

在 Grafana 导入 [dashboard/egress-watch.json](dashboard/egress-watch.json)，依次选择 **Prometheus datasource → Job → Instance → Port**。10 个面板覆盖建连速率、成功建连 p95、时间范围内计数、采集错误、就绪状态和进程内存。

| 指标 | 标签 | 含义 |
| --- | --- | --- |
| `egress_connections_total` | `port,event` | 通过过滤的尝试、成功、未建立即关闭事件数 |
| `egress_connect_duration_seconds` | `port` | 成功建连耗时 histogram，单位秒 |
| `egress_kernel_errors_total` | `reason` | `ringbuf_full`、`pending_map_full`、`namespace_read` |
| `egress_decode_errors_total` | — | 事件解码或校验失败 |
| `egress_log_errors_total` | — | JSON 写入失败；发生后采集器退出 |
| `egress_collector_ready` | — | 挂载并运行时为 1 |

同时提供 Go/process 指标。节点维度使用 `instance` 或抓取 relabel；IP、PID、cgroup 明细只放入日志。

```promql
sum by (instance, port) (rate(egress_connections_total{event="established"}[5m]))
```

> [!NOTE]
> 这些指标统计 TCP 连接事件，不代表 HTTP 请求数或当前活跃连接数。尝试与结果可能跨抓取窗口，不宜直接计算精确成功率；重启后使用 `rate` / `increase` 处理计数重置。

ring buffer 为 4 MiB，pending map 上限为 65536 条。缓冲区或 map 满时可能丢失事件或关联结果；内核错误在用户态 CIDR 过滤前统计，每秒同步，范围比最终业务指标更广。

## Kubernetes 部署

在仓库根目录构建并推送镜像：

```sh
docker build -t your-registry/egress-watch:v1 .
docker push your-registry/egress-watch:v1
```

构建默认使用当前平台，跨架构可用 `docker buildx build --platform linux/amd64` 或 `linux/arm64`。若默认 Go 代理不可达，可添加 `--build-arg GOPROXY=https://goproxy.cn,direct`。

编辑 [deploy/daemonset.yaml](deploy/daemonset.yaml) 中的镜像和 CIDR，再部署：

```sh
kubectl apply -f deploy/daemonset.yaml
kubectl -n kube-system logs -l app=egress-watch --tail=20
```

模板使用 privileged、hostNetwork，并只读挂载节点 BTF、tracefs 和 `/proc`。节点须预先挂载 tracefs；采集器通过 `/host/proc/1/ns/net` 识别宿主机 netns，无需 Kubernetes API 权限。

Prometheus annotations 需配合已有 Pod discovery 抓取配置。节点端口 `9359` 须保持可用，并按监控网络访问范围配置节点防火墙。

## 能力边界

| 场景 | 当前行为 |
| --- | --- |
| 普通 Pod / 容器主动出站 TCP | 采集匹配来源 CIDR 和目标规则的连接 |
| 同一 Pod 内多个容器 | 共享 Pod IP；保留 PID / cgroup 线索，未自动解析 Pod 名称或 container ID |
| 宿主机与 hostNetwork Pod | 排除 |
| 入站、启动前已有连接 | 不采集，不提供活跃连接表查询 |
| 进入 `SYN_SENT` 前失败 | 不计入，如部分无路由错误 |
| UDP / QUIC、HTTP 请求内容 | 不采集，连接复用不会产生逐请求事件 |
| SNAT 映射 | 不记录，不能直接从外部 SNAT 端口反查请求 |
| Sidecar / 出站代理 / CNI socket 重写 | 记录 socket 视角来源与目标，须在目标环境核对含义 |

## 开发与验证

```sh
make test
go vet ./...
python3 scripts/check-dashboard.py
```

macOS 上可运行 `go run ./cmd/egress-watch -config config.yaml -check-config`，该命令只校验配置。

真实容器烟测需要 Docker、Python 3、privileged 容器权限，以及与 Docker Linux 架构匹配的 `build/egress-watch` 和 BPF 对象。可用 Docker 工具链生成产物：

```sh
docker build --target toolchain -t conntrack-watch-egress-toolchain:dev .
docker run --rm -v "$PWD:/src" -w /src conntrack-watch-egress-toolchain:dev make build GOARCH=arm64
# Docker Linux VM 为 amd64 时，上面的 GOARCH 改为 amd64
docker pull python:3.12-alpine
python3 scripts/smoke-docker.py
```

脚本创建独立网络和测试容器，核对两个来源 IP、成功/拒绝连接、源端口、过滤和 JSON/metrics 一致性，结束时清理资源。证据位于忽略的 `build/smoke/`；结果与未覆盖项见 [VALIDATION.md](VALIDATION.md)。

```text
bpf/egress.c            内核 tracepoint 程序
cmd/egress-watch/       CLI 与 HTTP 服务入口
internal/egress/        配置、加载、事件、日志与指标
config.yaml            节点运行配置示例
deploy/daemonset.yaml   Kubernetes 部署模板
dashboard/             Grafana 看板
scripts/               静态检查与真实容器烟测
Makefile / Dockerfile  本地构建与容器构建
VALIDATION.md          验证记录与待验收范围
```
