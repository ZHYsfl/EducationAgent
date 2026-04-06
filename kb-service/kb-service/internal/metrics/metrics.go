// Package metrics 提供 Prometheus 指标收集和导出。
// 所有模块通过导入此包注册指标，main.go 在 /metrics 端点统一暴露。
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ── HTTP 请求指标 ────────────────────────────────────────────────────────────

	// QueryLatency 查询延迟（向量检索 + 可选 rerank）
	QueryLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kb_query_latency_seconds",
			Help:    "Latency of RAG query requests in seconds",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		},
		[]string{"hybrid", "rerank"},
	)

	// QueryTotal 查询总次数（按 hybrid/rerank/status 标签，不含 user_id 避免高基数问题）
	QueryTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kb_query_total",
			Help: "Total number of RAG query requests",
		},
		[]string{"hybrid", "rerank", "status"},
	)

	// QueryChunksReturned 每次查询返回的 chunk 数量分布
	QueryChunksReturned = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kb_query_chunks_returned",
			Help:    "Number of chunks returned per RAG query",
			Buckets: []float64{1, 2, 5, 10, 15, 20},
		},
		[]string{"hybrid"},
	)

	// ── Worker 指标 ─────────────────────────────────────────────────────────────

	// WorkerQueueDepth Worker 队列当前积压深度
	WorkerQueueDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "kb_worker_queue_depth",
			Help: "Current number of jobs waiting in the worker queue",
		},
	)

	// WorkerDLQSize DLQ 当前积压数量
	WorkerDLQSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "kb_worker_dlq_size",
			Help: "Current number of jobs in the Dead Letter Queue",
		},
	)

	// WorkerIndexTotal 索引任务总计（按结果类型标签）
	WorkerIndexTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "kb_worker_index_total",
			Help: "Total number of indexing jobs processed by the worker",
		},
		[]string{"result"}, // "success" | "failed" | "dlq"
	)

	// WorkerIndexLatency 单个文档索引耗时（从 job 入队到 indexed 状态）
	WorkerIndexLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kb_worker_index_latency_seconds",
			Help:    "Latency of single document indexing in seconds",
			Buckets: []float64{1, 2, 5, 10, 30, 60, 120, 300},
		},
		[]string{"stage"}, // "parse" | "embed" | "upsert"
	)

	// ── 依赖健康指标 ─────────────────────────────────────────────────────────────

	// EmbedServiceLatency Embedding 服务延迟
	EmbedServiceLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kb_embed_latency_seconds",
			Help:    "Latency of embedding service calls in seconds",
			Buckets: []float64{.1, .25, .5, 1, 2, 5, 10},
		},
		[]string{"status"},
	)

	// RerankerLatency Rerank 服务延迟
	RerankerLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "kb_rerank_latency_seconds",
			Help:    "Latency of rerank service calls in seconds",
			Buckets: []float64{.05, .1, .2, .5, 1, 2, 5},
		},
		[]string{"status"},
	)
)
