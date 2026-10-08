# eBPF Container Egress Watch

当前为 `ebpf` 分支，主要工具是独立的 [eBPF 容器出站观测器](ebpf/README.md)，用于记录发起 TCP 连接的容器 IP、目标、建连结果和耗时。

从仓库根目录执行 `make -C ebpf build GOARCH=amd64`（ARM64 使用 `GOARCH=arm64`），生成 `ebpf/build/egress-watch` 和 BPF 对象。运行与部署请按 [eBPF 快速开始](ebpf/README.md#快速开始) 配置；[验证记录](ebpf/VALIDATION.md)说明已验证范围。

以下保留原有 conntrack 工具的说明，其根目录构建命令、配置和指标对应 conntrack 实现。

## Conntrack Watch

Linux 连接跟踪（conntrack）监控工具，用于实时监控指定端口的新连接，并提供 Prometheus 指标和 Web 查询界面。

## 项目背景

在容器化业务环境中，当 Nginx 作为七层入口时，经常遇到一个问题：通过 Nginx request log 排查请求来源时，日志中只能看到 Kubernetes Node 节点的 IP，而不是实际发起请求的 Pod IP，这给问题排查带来了很大困难。

**现有方案对比：**

| 方案                        | 描述                                                 | 问题                                          |
| --------------------------- | ---------------------------------------------------- | --------------------------------------------- |
| **BGP 网络**          | 使用 Calico BGP 将容器网段宣告给上联路由             | 生产网络不支持 BGP                            |
| **修改代码**          | 在 HTTP 客户端添加 Pod 名称到 UA（通过环境变量注入） | 涉及多种开发语言，工作量大                    |
| **链路追踪**          | 使用 SkyWalking/OpenTelemetry 等                     | 采样率问题，100% 采集成本过高，且需全应用接入 |
| **Conntrack 方案 ✅** | 监听连接跟踪表，记录五元组到 ES                      | 无侵入式，扩展性强，存储成本低                |

**本项目采用 Conntrack 方案：**

1. 通过监听 Linux conntrack 表的新增连接事件，过滤 Nginx 端口的连接
2. 将五元组信息（源IP、源端口、目标IP、目标端口、协议）记录为 JSON 日志
3. 通过 Filebeat 采集日志到 Elasticsearch
4. Nginx 配置添加 `$remote_port` 变量，查询时根据源 IP + 源端口在 ES 中关联查询，即可获取真实 Pod IP

**优势：** 无侵入式、扩展简单（可支持 MySQL、MongoDB 等）、存储成本低（仅保留 7-14 天数据即可）

## 功能特性

- 🔍 **实时监控** - 监听 conntrack 表的新增连接事件
- 📊 **Prometheus 指标** - 按端口统计连接数 `conntrack_new_connections_total{port="443"}`
- 📝 **JSON 日志** - 结构化日志输出，便于 Filebeat 采集到 ES
- 🌐 **Web 查询** - 通过五元组查询连接状态和 SNAT 信息（可选）
- ⚙️ **YAML 配置** - 灵活的配置文件

## 项目结构

```text
├── cmd/conntrack-watch/main.go    # 程序入口
├── internal/
│   ├── config/                    # 配置加载
│   ├── conntrack/                 # 连接监控与查询
│   ├── logger/                    # zap 日志
│   └── metrics/                   # Prometheus 指标
├── web/
│   ├── handler.go                 # HTTP 处理器
│   └── static/index.html          # 查询页面
├── config.yaml                    # 配置文件
└── go.mod
```

## 快速开始

### 编译

```bash
go build -o conntrack-watch ./cmd/conntrack-watch
```

### 配置

编辑 `config.yaml`：

```yaml
ports:
  - 80
  - 443

log:
  path: "/var/log/nat-tracker/nat.log"
  max_size_mb: 100
  max_backups: 10
  max_age_days: 7
  compress: true

prometheus:
  enabled: true
  listen_addr: ":9358"

web_ui:
  enabled: false  # 是否启用 Web 查询页面
```

### 运行

```bash
# 运行（需要 root 权限，日志目录会自动创建）
sudo ./conntrack-watch -config config.yaml
```

## Web 服务

默认监听 `:9358`，提供以下端点：

| 路径                     | 说明                                            |
| ------------------------ | ----------------------------------------------- |
| `/`                    | Web 查询页面（需配置 `web_ui.enabled: true`） |
| `/api/conntrack/query` | 连接查询 API                                    |
| `/metrics`             | Prometheus 指标                                 |

### 查询 API

```bash
curl "http://localhost:9100/api/conntrack/query?protocol=tcp&src_ip=10.0.0.5&src_port=45678&dst_ip=1.2.3.4&dst_port=443"
```

响应示例：

```json
{
  "status": "ESTABLISHED",
  "origin": {"src": "10.0.0.5", "dst": "1.2.3.4", "src_port": 45678, "dst_port": 443},
  "reply": {"src": "1.2.3.4", "dst": "192.168.1.100", "src_port": 443, "dst_port": 12345}
}
```

## 日志格式

JSON 格式输出，通过 `type` 字段区分日志类型：

**连接跟踪日志 (type: conntrack)**：

```json
{"ts":"2024-12-09T15:05:00.123+0800","level":"info","msg":"new_connection","type":"conntrack","dst_port":443,"src_ip":"10.0.0.5","src_port":45678,"dst_ip":"1.2.3.4","snat_ip":"192.168.1.100","snat_port":12345}
```

**普通日志 (type: log)**：

```json
{"ts":"2024-12-09T15:05:00.123+0800","level":"info","msg":"程序启动","type":"log"}
```

## Prometheus 指标

```text
# 按端口统计的新连接数
conntrack_new_connections_total{port="80"} 123
conntrack_new_connections_total{port="443"} 456
```

### PromQL 查询示例

**查看各端口每秒新建连接速率：**

```promql
rate(conntrack_new_connections_total[5m])
```

**按端口分别查看连接速率：**

```promql
# 443 端口每秒新建连接数
rate(conntrack_new_connections_total{port="443"}[5m])

# 80 端口每秒新建连接数
rate(conntrack_new_connections_total{port="80"}[5m])
```

**对比不同端口的连接趋势（适合 Grafana 图表）：**

```promql
sum by (port) (rate(conntrack_new_connections_total[5m]))
```

**查看过去 1 小时内连接数增长量：**

```promql
increase(conntrack_new_connections_total[1h])
```

**按实例和端口统计（多节点部署时）：**

```promql
sum by (instance, port) (rate(conntrack_new_connections_total[5m]))
```

## 依赖

- Linux 系统（conntrack 模块）
- Root 权限
- Go 1.21+

## License

MIT
