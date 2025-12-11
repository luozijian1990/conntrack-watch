package conntrack

import (
	"context"
	"os"

	ct "github.com/florianl/go-conntrack"

	"conntrack-watch-new/internal/logger"
	"conntrack-watch-new/internal/metrics"
)

// Watcher 连接跟踪监控器
type Watcher struct {
	nfct        *ct.Nfct
	targetPorts map[uint16]bool
}

// NewWatcher 创建新的监控器
func NewWatcher(ports []uint16) (*Watcher, error) {
	nfct, err := ct.Open(&ct.Config{
		NetNS: 0,
	})
	if err != nil {
		return nil, err
	}

	targetPorts := make(map[uint16]bool)
	for _, port := range ports {
		targetPorts[port] = true
	}

	return &Watcher{
		nfct:        nfct,
		targetPorts: targetPorts,
	}, nil
}

// Close 关闭监控器
func (w *Watcher) Close() error {
	return w.nfct.Close()
}

// GetNfct 获取底层 nfct 实例（用于查询 API）
func (w *Watcher) GetNfct() *ct.Nfct {
	return w.nfct
}

// Start 开始监控
func (w *Watcher) Start(ctx context.Context) error {
	callback := func(c ct.Con) int {
		if c.Origin == nil || c.Origin.Proto == nil {
			return 0
		}

		// 只处理 TCP (protocol = 6)
		if *c.Origin.Proto.Number != 6 {
			return 0
		}

		if c.Origin.Proto.DstPort == nil {
			return 0
		}
		dstPort := *c.Origin.Proto.DstPort

		if !w.targetPorts[dstPort] {
			return 0
		}

		// 获取 IP 信息
		srcIP := ""
		dstIP := ""
		if c.Origin.Src != nil {
			srcIP = c.Origin.Src.String()
		}
		if c.Origin.Dst != nil {
			dstIP = c.Origin.Dst.String()
		}

		srcPort := uint16(0)
		if c.Origin.Proto.SrcPort != nil {
			srcPort = *c.Origin.Proto.SrcPort
		}

		// 获取 SNAT 后的信息
		snatIP := ""
		snatPort := uint16(0)
		if c.Reply != nil && c.Reply.Dst != nil {
			snatIP = c.Reply.Dst.String()
		}
		if c.Reply != nil && c.Reply.Proto != nil && c.Reply.Proto.DstPort != nil {
			snatPort = *c.Reply.Proto.DstPort
		}

		// 记录日志和指标
		logger.LogConnection(dstPort, srcIP, srcPort, dstIP, snatIP, snatPort)
		metrics.RecordNewConnection(dstPort)

		return 0
	}

	err := w.nfct.Register(ctx, ct.Conntrack, ct.NetlinkCtNew, callback)
	if err != nil {
		logger.Log.Error("注册监听失败: " + err.Error())
		os.Exit(1)
	}

	return nil
}
