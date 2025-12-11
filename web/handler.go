package web

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"conntrack-watch-new/internal/conntrack"
	"conntrack-watch-new/internal/logger"
)

//go:embed static
var staticFiles embed.FS

// Server Web 服务器
type Server struct {
	watcher *conntrack.Watcher
}

// NewServer 创建 Web 服务器
func NewServer(watcher *conntrack.Watcher) *Server {
	return &Server{watcher: watcher}
}

// Start 启动 HTTP 服务
func (s *Server) Start(listenAddr string, webUIEnabled bool) {
	// Prometheus 指标
	http.Handle("/metrics", promhttp.Handler())

	// API 路由
	http.HandleFunc("/api/conntrack/query", s.handleQuery)

	// 静态文件（仅在启用时注册）
	if webUIEnabled {
		staticFS, _ := fs.Sub(staticFiles, "static")
		http.Handle("/", http.FileServer(http.FS(staticFS)))
		logger.Log.Info("Web UI 已启用")
	}

	go func() {
		logger.Log.Info("Web 服务已启动: " + listenAddr)
		if err := http.ListenAndServe(listenAddr, nil); err != nil {
			logger.Log.Error("Web 服务错误: " + err.Error())
		}
	}()
}

// handleQuery 处理连接查询请求
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	query := r.URL.Query()

	srcPort, _ := strconv.ParseUint(query.Get("src_port"), 10, 16)
	dstPort, _ := strconv.ParseUint(query.Get("dst_port"), 10, 16)

	params := conntrack.QueryParams{
		Protocol: query.Get("protocol"),
		SrcIP:    query.Get("src_ip"),
		DstIP:    query.Get("dst_ip"),
		SrcPort:  uint16(srcPort),
		DstPort:  uint16(dstPort),
	}

	if params.Protocol == "" {
		params.Protocol = "tcp"
	}

	result, err := s.watcher.Query(params)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if result == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "connection not found"})
		return
	}

	json.NewEncoder(w).Encode(result)
}
