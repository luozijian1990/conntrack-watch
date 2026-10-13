# eBPF 分支验证记录

日期：2026-10-02。实现范围：主动 TCP 出站连接的容器来源 IP、JSON 日志、Prometheus 指标、Grafana dashboard、构建和部署模板。

## 2026-10-13 tracepoint 布局兼容与单文件部署

针对 5.14 发行版内核报告 `saddr offset=40`、原代码预期 32 的启动失败，改为从运行内核的 tracefs format 解析必需字段偏移，在加载前写入 BPF 只读常量，使用 `bpf_probe_read_kernel` 按实际偏移读取。仍校验字段大小、缺失、重复、范围和重叠；增加 `tracepoint_read` 健康指标。BPF 编译显式使用 `-mcpu=v1`，兼容目标调整为启用所需能力的 Linux 5.10+、amd64/arm64 小端节点。

`log_attempts` 默认为 `false`（省略时也为 false），示例配置与 DaemonSet 模板保持一致，默认只输出建连结果。服务器验收为观察两个阶段曾显式开启 `true`；人工确认后，dev105 当前配置已为 `false`，原始验收配置与日志仍保留。

BPF ELF 通过 `go:embed` 嵌入 Linux 二进制，配置、部署模板和运行镜像不再需要 `object_path` 或独立 `.o`。`make build` 自动编译并嵌入；Linux `make test` 同样先编译对象。CI 分别上传 amd64 / arm64 可执行文件。

本轮验证：

- 修改前，偏移整体增加 8/16 字节的回归测试失败；修改后通过，另覆盖仅地址字段偏移变化（`saddr=40`）、数组字段名和不安全布局拒绝。
- macOS 和 Linux 上的 `go test -race ./...`、`go vet ./...` 通过；Linux 测试还验证嵌入 ELF 解析、Go/C 偏移常量 ABI 与健康 map 大小。
- Linux Clang BPF 编译和 linux/amd64、linux/arm64 静态二进制构建通过；配置校验、dashboard 静态检查和 diff 检查通过。
- Docker Linux `6.12.76-linuxkit` / arm64 上，嵌入对象真实加载，两个来源容器产生 10 条事件，成功/拒绝连接、源端口补全、目标/端口/host netns 过滤、JSON/Prometheus 一致和 SIGTERM 清理通过，全部采集健康错误为 0。证据位于忽略的 `build/smoke/`。

已在 `dev105` 的 `5.14.0-362.8.1.el9_3.x86_64` 上完成实际加载与容器出站验证：公共头含 `common_preempt_lazy_count`，`skaddr=16`、`saddr=40`，必需字段整体后移 8 字节。单文件 amd64 二进制无需独立 `.o` 成功挂载。

验证配置使用默认 Docker bridge `172.17.0.0/16`，目标端口 443，开启 attempt 日志，HTTP 监听 `127.0.0.1:9359`。2026-10-13 16:15:34（Asia/Singapore）在 `test-nginx`（`172.17.0.2`）执行 `curl https://www.baidu.com`，退出码 0、HTTP 200；日志记录 attempt 和 established 共 2 条，建连结果为 `172.17.0.2:50906 -> 183.240.99.224:443`，`comm=curl`、耗时 10.736147 ms，与 curl 的 local/remote IP、端口一致。四种 kernel error 指标均为 0，采集器 SIGTERM 后退出码 0，测试结束后已停止。

服务器证据保留于 `/root/conntrack-watch/build/validation-20261013-161533/`，包括 `connections.jsonl`、`collector.log`、`curl.log`、`metrics.txt`、`report.json`、实际 tracepoint format、网络信息和原文件备份 `original-files.tar.gz`；当前验证配置在 `/root/conntrack-watch/config.yaml`。本地副本在忽略的 `build/dev105-validation/validation-20261013-161533/`。服务器原有根目录 `egress-watch` 未覆盖，本次使用 `build/egress-watch-linux-amd64`。

尚未在真实 5.10 节点加载，未验证 Kubernetes/CNI、IPv6 真实流量或负载。本轮未重建最终运行镜像。版本目标不是对所有发行版内核配置的保证，仍要求 BTF、BPF、tracepoint、ring buffer 与足够权限。

## 2026-10-08 根目录迁移复验

`ebpf` 分支现以仓库根目录作为唯一 Go 模块、构建和配置入口。移除本分支的旧 conntrack 程序、Web 查询页面、架构/流程图和旧 CI；原实现仍在 `main` 分支。eBPF 源码、测试、配置、Dockerfile、Makefile、部署模板、看板和脚本与迁移前逐文件比对一致。README 重新整理，CI 改用根目录路径，Docker 构建上下文排除 `.git`、`.vscode` 和 `build`。

本轮从根目录实际执行并通过：

- `make test`（`go test -race ./...`）、`go vet ./...`、宿主机 `go run ./cmd/egress-watch -config config.yaml -check-config`。
- 宿主机 Go 交叉编译 linux/amd64、linux/arm64；现有 Linux 工具链容器中分别执行 `make build GOARCH=amd64` 和 `GOARCH=arm64`，包括 BPF C 编译。
- `python3 scripts/check-dashboard.py`：10 个面板静态检查；README 本地文件链接检查、`git diff --check`。
- `docker build -t conntrack-watch-egress:root-migration-check .`：arm64 最终运行镜像构建及镜像内配置校验。此次默认 Go 代理可用。
- 使用本轮 arm64 可执行文件和 BPF 对象执行 `python3 scripts/smoke-docker.py`：Docker Linux `6.12.76-linuxkit`，两个来源 IP `172.24.0.3`、`172.24.0.4`，共 10 条事件；3 次成功、2 次拒绝，源端口补全、目标/端口/host netns 排除、JSON 与 Prometheus 一致、零采集错误、SIGTERM 退出码 0 均通过。测试容器和网络已由脚本清理，证据更新于 `build/smoke/`。

本轮未重新执行 promtool 查询解析、Grafana 页面验证、目标 Kubernetes/CNI、IPv6 实际流量或负载验收。下面的早期记录保留当时的目录结构和验证范围。

## 2026-10-08 分支提交修复复验

修复前 `main` 与 `ebpf` 均指向 `6593372`，`ebpf/` 和 eBPF CI 尚未被 Git 跟踪，分支提交不包含采集器入口。此次将独立模块及 CI 纳入 `ebpf` 分支，并在根 README 标明实际构建和使用入口。

本次重新通过 `go test -race ./...`、`go vet ./...`、linux/amd64 与 linux/arm64 交叉编译、`-check-config`、10 个 dashboard 面板静态检查，以及已有 Linux 工具链容器中的 BPF 编译。本机 Go 构建缓存受沙箱限制，交叉编译使用临时 `GOCACHE` 完成。

使用本次编译的 arm64 程序和 BPF 对象重新执行 `python3 scripts/smoke-docker.py`，在 `6.12.76-linuxkit` 上通过真实 CO-RE/tracepoint 挂载、两个来源容器、成功/拒绝连接、源端口补全、目标/端口/host netns 排除、JSON 与指标一致、零采集错误和 SIGTERM 清理；产生 10 条事件。证据更新于忽略的 `build/smoke/`。本次未重建最终运行镜像，也未执行 Kubernetes/CNI、IPv6 实际流量或负载验收。

## 早期验证记录（2026-10-02）

| 验证 | 结果与证据范围 |
|---|---|
| Go 单元测试 | `go test -race ./...` 通过；事件解码、IPv4/IPv6/mapped 地址、时间换算、未知源端口、JSON、CIDR/端口过滤、严格配置、指标口径、tracepoint ABI 校验 |
| Go 静态检查 | `go vet ./...` 通过 |
| Linux 构建 | 本地 Go 1.22.2 交叉编译 linux/amd64 和 linux/arm64 成功 |
| BPF C 编译 | Linux 容器中的 Clang 14，`-target bpfel -O2 -g -Wall -Werror` 成功 |
| 真实内核加载 | Docker Desktop Linux VM `6.12.76-linuxkit` / arm64；BTF CO-RE、verifier、maps、ring buffer、`sock:inet_sock_set_state` 挂载成功 |
| 真实容器流量 | `python3 scripts/smoke-docker.py` 通过；2 个独立来源容器，3 次成功连接（含预热）和 2 次拒绝连接 |
| 来源与过滤 | 两个源 IP 分别为 `172.24.0.3`、`172.24.0.4`；实际客户端源端口与结果 JSON 一致；目标排除、未配置端口、宿主机 netns 排除通过 |
| 日志与指标 | 共 10 条 JSON（5 attempts + 5 outcomes）；metrics 为 3 established、2 closed_before_established；成功耗时样本数为 3 |
| 采集健康 | 上述低流量测试中 decode / ringbuf_full / pending_map_full / namespace_read 均为 0；SIGTERM 后采集器退出码为 0 |
| 运行镜像 | `conntrack-watch-egress:dev` / arm64 构建成功，最终 distroless 镜像 `-check-config` 执行成功 |
| Dashboard | 10 个面板静态检查通过；用 Prometheus v3.5.0 `promtool check rules` 解析替换变量后的 10 条查询，全部通过 |

默认 `proxy.golang.org` 在本机 Docker 网络中超时；通过 Dockerfile 提供的 `GOPROXY` build arg 指定 `https://goproxy.cn,direct` 后完成最终镜像构建。没有改动宿主机的全局 Go 代理配置。

## 证据文件

运行时证据位于忽略的 `build/smoke/`，不会将环境相关产物纳入源码：

- `report.json`：实际内核、IP、断言结果与验证边界。
- `connections.jsonl`：真实 tracepoint 产生的连接事件。
- `metrics.txt`：对应 `/metrics` 抓取结果。
- `config.yaml`：该次隔离测试配置。

测试脚本在结束时删除其创建的容器和网络；没有发布宿主机端口，没有部署到 Kubernetes。

## 尚未验证

- 真实 5.10 内核兼容性，以及除已验收 dev105 5.14 / amd64 和 Docker 6.12 / arm64 以外的目标发行版。
- 真实 Kubernetes Pod/CNI、Service Mesh/sidecar、出站代理、CNI socket 重写。
- IPv6 的真实内核流量（仅解码与 CIDR 单元测试覆盖）。
- 跨节点网络与更多 NAT 拓扑；dev105 已验证默认 Docker bridge 容器到公网百度 HTTPS 的出站连接。
- 高并发容量、ring buffer 溢出/丢失注入、pending map 满、节点长时间运行和资源上限。
- Grafana 页面实际导入、变量交互、真实 Prometheus 抓取配置。
- Kubernetes DaemonSet 的实际部署和运行镜像的完整集群验收。

上线前先修改真实 source/exclude CIDR，再在目标节点跑一组已知来源的成功、拒绝、超时、排除目标和长连接复用流量，核对 JSON、metrics 和采集错误。对代理场景应额外核对记录的来源进程与目标是否符合预期。
