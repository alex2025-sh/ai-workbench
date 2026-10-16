// Package obs 是可观测性的公共约定：指标名、日志与 trace 的公共字段。
package obs

// 日志与 trace 的公共字段，每个 job 都应带上。
const (
	FieldRunID     = "run_id"
	FieldNodeID    = "node_id"
	FieldIteration = "iteration"
	FieldTaskID    = "task_id"
)

// 指标名。
const (
	MetricJobWait         = "wf_job_wait_seconds"         // NOW() - due_at
	MetricClaimConflict   = "wf_job_claim_conflict_total" // CAS 认领冲突
	MetricNodeFailure     = "wf_node_failure_total"
	MetricNeedsIntervene  = "wf_run_needs_intervention"
	MetricHumanWait       = "wf_human_wait_seconds" // 人工节点等待时长
	MetricNotifyToContact = "task_notify_to_contact_seconds"
	MetricSummaryFailure  = "view_ai_summary_failure_total"
)
