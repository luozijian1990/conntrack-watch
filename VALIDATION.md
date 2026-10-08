# eBPF 分支验证记录

日期：2026-10-02。实现范围：主动 TCP 出站连接的容器来源 IP、JSON 日志、Prometheus 指标、Grafana dashboard、构建和部署模板。

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

## 已执行并通过

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

- 用户目标 Linux 发行版/内核版本、amd64 实际加载、5.15 最低目标兼容性。
- 真实 Kubernetes Pod/CNI、Service Mesh/sidecar、出站代理、CNI socket 重写。
- IPv6 的真实内核流量（仅解码与 CIDR 单元测试覆盖）。
- 真正跨节点/跨网络 NAT 场景；目前真实流量用同一隔离 Docker 网络内的不同容器模拟来源/目标。不会将它称为生产外部服务链路验收。
- 高并发容量、ring buffer 溢出/丢失注入、pending map 满、节点长时间运行和资源上限。
- Grafana 页面实际导入、变量交互、真实 Prometheus 抓取配置。
- Kubernetes DaemonSet 的实际部署和运行镜像的完整集群验收。

上线前先修改真实 source/exclude CIDR，再在目标节点跑一组已知来源的成功、拒绝、超时、排除目标和长连接复用流量，核对 JSON、metrics 和采集错误。对代理场景应额外核对记录的来源进程与目标是否符合预期。
