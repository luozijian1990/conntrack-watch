package conntrack

import (
	"net"
	"strings"

	ct "github.com/florianl/go-conntrack"
)

// ConnectionInfo 连接信息响应
type ConnectionInfo struct {
	Status string    `json:"status,omitempty"`
	Origin TupleInfo `json:"origin"`
	Reply  TupleInfo `json:"reply"`
}

// TupleInfo IP 元组信息
type TupleInfo struct {
	Src     string `json:"src"`
	Dst     string `json:"dst"`
	SrcPort uint16 `json:"src_port"`
	DstPort uint16 `json:"dst_port"`
}

// QueryParams 查询参数
type QueryParams struct {
	Protocol string
	SrcIP    string
	DstIP    string
	SrcPort  uint16
	DstPort  uint16
}

// Query 查询指定连接的状态
func (w *Watcher) Query(params QueryParams) (*ConnectionInfo, error) {
	// 为查询创建独立的 nfct 连接，避免与监听冲突
	nfct, err := ct.Open(&ct.Config{})
	if err != nil {
		return nil, err
	}
	defer nfct.Close()

	srcIPAddr := net.ParseIP(params.SrcIP)
	dstIPAddr := net.ParseIP(params.DstIP)

	var queryProtocol uint8
	switch params.Protocol {
	case "tcp":
		queryProtocol = 6
	case "udp":
		queryProtocol = 17
	default:
		queryProtocol = 6
	}

	match := ct.Con{
		Reply: &ct.IPTuple{
			Src: &srcIPAddr,
			Dst: &dstIPAddr,
			Proto: &ct.ProtoTuple{
				Number:  &queryProtocol,
				SrcPort: &params.SrcPort,
				DstPort: &params.DstPort,
			},
		},
	}

	res, err := nfct.Get(ct.Conntrack, ct.IPv4, match)
	if err != nil {
		// netlink 错误通常表示连接不存在
		if strings.Contains(err.Error(), "no such file or directory") ||
			strings.Contains(err.Error(), "netlink") {
			return nil, nil // 返回 nil 表示连接未找到
		}
		return nil, err
	}

	if len(res) == 0 {
		return nil, nil
	}

	conn := res[0]
	info := &ConnectionInfo{}

	// 解析 Origin
	if conn.Origin != nil {
		if conn.Origin.Src != nil {
			info.Origin.Src = conn.Origin.Src.String()
		}
		if conn.Origin.Dst != nil {
			info.Origin.Dst = conn.Origin.Dst.String()
		}
		if conn.Origin.Proto != nil {
			if conn.Origin.Proto.SrcPort != nil {
				info.Origin.SrcPort = *conn.Origin.Proto.SrcPort
			}
			if conn.Origin.Proto.DstPort != nil {
				info.Origin.DstPort = *conn.Origin.Proto.DstPort
			}
		}
	}

	// 解析 Reply
	if conn.Reply != nil {
		if conn.Reply.Src != nil {
			info.Reply.Src = conn.Reply.Src.String()
		}
		if conn.Reply.Dst != nil {
			info.Reply.Dst = conn.Reply.Dst.String()
		}
		if conn.Reply.Proto != nil {
			if conn.Reply.Proto.SrcPort != nil {
				info.Reply.SrcPort = *conn.Reply.Proto.SrcPort
			}
			if conn.Reply.Proto.DstPort != nil {
				info.Reply.DstPort = *conn.Reply.Proto.DstPort
			}
		}
	}

	// 解析状态
	if conn.ProtoInfo != nil && conn.ProtoInfo.TCP != nil && conn.ProtoInfo.TCP.State != nil {
		info.Status = tcpStateToString(*conn.ProtoInfo.TCP.State)
	}

	return info, nil
}

func tcpStateToString(state uint8) string {
	states := map[uint8]string{
		0:  "NONE",
		1:  "SYN_SENT",
		2:  "SYN_RECV",
		3:  "ESTABLISHED",
		4:  "FIN_WAIT",
		5:  "CLOSE_WAIT",
		6:  "LAST_ACK",
		7:  "TIME_WAIT",
		8:  "CLOSE",
		9:  "SYN_SENT2",
		10: "MAX",
	}
	if s, ok := states[state]; ok {
		return s
	}
	return "UNKNOWN"
}
