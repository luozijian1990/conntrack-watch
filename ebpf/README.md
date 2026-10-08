# eBPF Container Egress Watch

**在连接发起侧，查清哪个容器 IP 访问了哪个外部服务。**

基于 eBPF tracepoint 的节点级 TCP 出站观测工具。无需修改应用，即可输出连接来源、目标、建连结果和耗时，并提供 JSON 日志、Prometheus 指标与 Grafana 看板。

[快速开始](#快速开始) · [配置](#配置) · [JSON 日志](#json-日志) · [监控看板](#监控看板) · [Kubernetes 部署](#kubernetes-部署) · [能力边界](#能力边界)

## 为什么需要它

容器访问外部 Nginx、数据库等服务时，经过节点 SNAT，服务端日志往往只能看到 Node IP。排查来源时，我们真正需要的是：

```text
容器发起：10.244.1.23:45678 → 172.16.10.50:443
                │
                ├── eBPF 在发起侧记录容器 IP、目标和建连结果
                │
                └── 节点 SNAT → 外部服务看到 Node IP
```

Egress Watch 部署在容器所在节点，通过 `sock:inet_sock_set_state` 观察主动 TCP 建连。连接明细交给 Filebeat / Elasticsearch，连接趋势与采集健康交给 Prometheus / Grafana。

| 能力 | 输出 |
| --- | --- |
| 来源定位 | 源 IP、源端口、网络命名空间、发起进程 PID / comm / cgroup ID |
| 建连观测 | 尝试、成功、未建立即关闭，以及结果事件的耗时 |
| 范围过滤 | 目标端口、来源 CIDR、目标 CIDR 和目标排除范围 |
| 日志采集 | 逐行 JSON，支持 stdout 或轮转文件 |
| 运行监控 | 连接指标、成功建连延迟、事件丢失与采集器状态 |

此目录是独立 Go 模块，构建入口为 `cmd/egress-watch`。根目录的 [conntrack 实现](../README.md)保留自己的配置、日志和指标。

## 快速开始

### 环境要求

| 项目 | 要求 |
| --- | --- |
| 运行节点 | Linux，amd64 或 arm64，小端 |
| 内核能力 | BTF、BPF、tracepoint、ring buffer；可读取 `/sys/kernel/btf/vmlinux` |
| 编译工具 | Go 1.21+、带 BPF 后端的 Clang、make |
| 运行权限 | 具备 BPF / tracepoint 权限；以下示例使用 root 或 privileged 容器 |

目标内核为 Linux 5.15+；当前真实加载和容器流量验证环境为 **Linux 6.12 / arm64**。其他内核、目标 Kubernetes/CNI 仍需验收，详见[验证记录](VALIDATION.md)。macOS 可运行 Go 单元测试和配置校验，采集需在 Linux 上执行。

### 1. 编译

从仓库根目录进入 `ebpf/`。本文后续命令均在该目录执行。

```sh
cd ebpf
make build GOARCH=amd64  # ARM64 节点改为 GOARCH=arm64
```

产物为 `build/egress-watch` 和 `build/egress.bpf.o`，运行时需要同时保留。

### 2. 配置观测范围

编辑 [config.yaml](config.yaml)，替换为真实的 Pod 和 Service 网段：

```yaml
ports: [80, 443]
source_cidrs: ["10.244.0.0/16"]
destination_cidrs: []
exclude_cidrs: ["10.244.0.0/16", "10.96.0.0/12", "127.0.0.0/8", "::1/128"]
```

> [!IMPORTANT]
> 示例 CIDR 不会自动适配集群。`source_cidrs` 决定采集哪些来源，`exclude_cidrs` 决定排除哪些目标。外部服务可以使用公司内网 IP，无需将所有私网地址排除。

### 3. 启动并检查

```sh
./build/egress-watch -config config.yaml -check-config

# 仅在节点尚未挂载 tracefs 时执行：
sudo mount -t tracefs tracefs /sys/kernel/tracing

sudo ./build/egress-watch -config config.yaml
```

在另一终端检查就绪状态和指标，再从匹配来源范围的容器发起一次目标端口连接：

```sh
curl -fsS http://127.0.0.1:9359/readyz
curl -fsS http://127.0.0.1:9359/metrics
```

默认连接日志输出到 stdout，运行诊断以 JSON 输出到 stderr。默认只记录建连结果；需要同时查看尝试事件时，设置 `log_attempts: true`。

## 配置

完整示例见 [config.yaml](config.yaml)。配置采用严格字段校验，修改后可用 `-check-config` 检查。

| 配置项 | 默认值 / 说明 |
| --- | --- |
| `ports` | `[80, 443]`；必须非空，端口不可为 0 或重复 |
| `source_cidrs` | 必填、非空；Pod / 容器来源网段，支持 IPv4 和 IPv6 |
| `destination_cidrs` | `[]`；允许的目标范围，空表示不限 |
| `exclude_cidrs` | `[]`；排除目标，优先于允许范围；随附配置已填写示例网段 |
| `object_path` | `build/egress.bpf.o`；相对路径以工作目录为基准 |
| `host_netns_path` | `/proc/1/ns/net`；必须指向真实宿主机网络命名空间 |
| `listen_addr` | `:9359`；HTTP 监听地址 |
| `node` | 未填写时依次使用 `NODE_NAME` 环境变量、hostname |
| `log_attempts` | `false`；是否额外输出尝试事件日志，不影响尝试计数 |
| `log.path` | 空表示 stdout；填写路径则输出到轮转文件 |
| `log.max_size_mb` | `100`；单文件大小上限 |
| `log.max_backups` / `log.max_age_days` | `10` / `7`；备份数与保留天数，旧日志压缩；三个轮转参数均须为正数 |

内核先按目标端口过滤并排除 host netns，Go 再进行 CIDR 过滤。IPv4-mapped IPv6 地址按 IPv4 CIDR 匹配，日志 `family` 保留 socket 的 `ipv6` 类型。

| HTTP 端点 | 用途 |
| --- | --- |
| `/metrics` | Prometheus 抓取 |
| `/healthz` | HTTP 服务存活检查 |
| `/readyz` | 采集器挂载并运行时返回 200，否则返回 503 |

## JSON 日志

以下示例为便于阅读展开显示；实际每条事件占一行：

```json
{
  "ts": "2026-10-02T10:00:00Z",
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
| `attempt` | 观察到主动进入 `SYN_SENT`；源端口可能尚未分配 |
| `established` | 关联到该 socket 进入 `ESTABLISHED`，补齐源端口 |
| `closed_before_established` | 观察到建连阶段后关闭；可能是拒绝、超时或应用取消，不提供具体失败原因 |

`src_port_known=false` 与 `src_port=0` 表示端口尚未确定。`connect_duration_ms` 仅在结果事件出现，表示从尝试到成功或关闭的时间。`ts` 从内核事件时间换算为 UTC，建连耗时不包含用户态排队时间。

`pid` 是发起进程的宿主机 TGID，`comm` 最长 15 字节，`cgroup_id` 是内核标识。`started_monotonic_ns` 可结合节点、netns、IP 和进程辅助关联尝试与结果，不能作为跨节点、跨重启的全局唯一 ID。

Filebeat 可按 `type=egress_connection` 筛选连接日志；在 ES 中按时间、`src_ip`、`dst_ip`、`dst_port` 和 `event` 查询来源明细。

## 监控看板

在 Grafana 导入 [egress-watch.json](dashboard/egress-watch.json)，选择 **Prometheus datasource → Job → Instance → Port**。10 个面板覆盖建连速率、成功建连 p95、时间范围内计数、采集错误、就绪状态和进程内存。

| 指标 | 标签 | 含义 |
| --- | --- | --- |
| `egress_connections_total` | `port,event` | 通过过滤的尝试、成功、未建立即关闭事件数 |
| `egress_connect_duration_seconds` | `port` | 成功建连耗时 histogram，单位秒 |
| `egress_kernel_errors_total` | `reason` | `ringbuf_full`、`pending_map_full`、`namespace_read` |
| `egress_decode_errors_total` | — | 事件解码或校验失败 |
| `egress_log_errors_total` | — | JSON 写入失败；发生后采集器退出 |
| `egress_collector_ready` | — | 挂载并运行时为 1 |

同时提供标准 Go/process 指标。节点维度使用 Prometheus `instance` 或抓取 relabel；IP、PID、cgroup 明细保留在日志中，避免增加指标基数。

```promql
# 各节点、端口每秒成功建连数
sum by (instance, port) (rate(egress_connections_total{event="established"}[5m]))

# 各端口成功建连 p95，单位秒
histogram_quantile(0.95, sum by (le, port) (rate(egress_connect_duration_seconds_bucket[5m])))
```

> [!NOTE]
> 指标统计 TCP 连接事件，不代表 HTTP 请求数或当前活跃连接数。尝试与结果可能跨抓取窗口，不宜直接计算精确成功率；重启会重置计数，应使用 `rate` / `increase`。

采集缓冲区为 4 MiB，pending map 上限为 65536 条。缓冲区或 map 满时会报告内核错误，可能丢失事件或关联结果；计数不保证无损。内核错误在用户态 CIDR 过滤前统计，每秒同步，范围比最终业务指标更广。

## Kubernetes 部署

构建镜像并推送到节点可访问的仓库：

```sh
docker build -t your-registry/egress-watch:v1 .
docker push your-registry/egress-watch:v1
```

构建默认使用当前平台，跨架构可使用 `docker buildx --platform`。若默认 Go 代理不可达，构建时可指定 `--build-arg GOPROXY=https://goproxy.cn,direct`。

编辑 [deploy/daemonset.yaml](deploy/daemonset.yaml) 中的镜像和 CIDR，再部署：

```sh
kubectl apply -f deploy/daemonset.yaml
kubectl -n kube-system logs -l app=egress-watch --tail=20
```

模板以 privileged DaemonSet 运行，使用 hostNetwork，并只读挂载节点 BTF、tracefs 和 `/proc`。节点需预先挂载 tracefs；采集器通过 `/host/proc/1/ns/net` 识别宿主机网络命名空间，无需 Kubernetes API 权限。

Prometheus annotations 需配合已有的 Pod discovery 抓取配置。端口 `9359` 应保持可用，并按监控网络的访问范围配置节点防火墙。

## 工作原理与能力边界

```text
CLOSE → SYN_SENT
  保存 socket 来源地址、netns、发起进程身份
  按 socket 关联后续状态
       ├── ESTABLISHED → 成功 + 源端口 + 耗时
       └── CLOSE       → 未建立即关闭 + 耗时
                    ↓ ring buffer
        Go 解码与 CIDR 过滤 → JSON / metrics
```

来源身份在主动建连时保存，后续状态变化沿用该身份；网络命名空间取自 socket，避免将软中断中执行的成功事件归属到错误进程。结果输出后删除 pending 记录。

| 场景 | 当前行为 |
| --- | --- |
| 普通 Pod / 容器出站 TCP | 观察匹配来源 CIDR 与目标规则的主动连接 |
| 同一 Pod 内多个容器 | 共享 Pod IP；保留 PID / cgroup 线索，未自动关联 Pod 名称或 container ID |
| 宿主机与 hostNetwork Pod | 排除 |
| 入站连接、启动前已有连接 | 不采集；不提供活跃连接表查询 |
| 进入 SYN_SENT 前失败 | 不计入，如部分无路由错误 |
| UDP / QUIC、HTTP 请求内容 | 不采集；连接复用不会产生逐请求事件 |
| SNAT 端口反查 | 不记录 NAT 映射，不能直接从外部 SNAT 端口反查到某个请求 |
| Sidecar / 出站代理 / CNI socket 重写 | 记录 socket 视角的来源与目标，需在目标环境核对含义 |

## 开发与验证

```sh
make test
go vet ./...
python3 scripts/check-dashboard.py
```

真实容器烟测需要 Docker、Python 3、privileged 容器权限，以及与 Docker Linux 架构匹配的 `build/egress-watch` 和 BPF object：

```sh
docker build --target toolchain -t conntrack-watch-egress-toolchain:dev .
docker pull python:3.12-alpine
python3 scripts/smoke-docker.py
```

脚本创建独立网络和测试容器，核对两个来源 IP、成功/拒绝连接、源端口、过滤和 JSON/metrics 一致性；结束清理测试资源，证据保存在 `build/smoke/`。完整结果与未覆盖项见 [VALIDATION.md](VALIDATION.md)，其中明确区分 Docker 实测、目标 Kubernetes 验收和 Grafana 页面验证。

```text
bpf/egress.c            内核 tracepoint 程序
cmd/egress-watch/       CLI 与 HTTP 服务入口
internal/egress/        配置、加载、事件、日志与指标
config.yaml            节点运行配置示例
deploy/daemonset.yaml   Kubernetes 部署模板
dashboard/             Grafana 看板
scripts/               静态检查与真实容器烟测
```
