package tasktypeservicelogic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/rpctest"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/dryrun"
	"github.com/alex2025-sh/ai-workbench/internal/flow/nodes"
)

// sample 构造一条 VIP 工单 payload；check_48h / check_7d 按 appeal_id 查 mock 业务库中的实时状态。
func sample(appealID, uid string, committedAgo time.Duration) []byte {
	return []byte(fmt.Sprintf(`{"appeal_id":%q,"appeal_type":"withdraw_review","appeal_status":"COMMITED","uid":%q,"vip_level":3,
		"order_amount":"25000.00","case_group":"vip_queue","reviewer":"reviewer_a","commit_time_ms":%d}`,
		appealID, uid, time.Now().Add(-committedAgo).UnixMilli()))
}

func count(t *testing.T, dsn, query string) int {
	t.Helper()
	var n int
	if err := sqlx.NewMysql(dsn).QueryRowCtx(context.Background(), &n, query); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDryRun(t *testing.T) {
	env := rpctest.New(t)
	ctx := rpctest.As("dev@example.com")
	tasksBefore := count(t, env.Platform, "select count(*) from task")
	runsBefore := count(t, env.Platform, "select count(*) from wf_run")

	dry := func(req *pb.DryRunTaskTypeDraftReq) *pb.DryRunResp {
		t.Helper()
		req.Key = "vip_appeal_alert"
		res, err := NewDryRunTaskTypeDraftLogic(ctx, env.Svc).DryRunTaskTypeDraft(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	ports := func(c *pb.DryRunCase) string {
		var out []string
		for _, s := range c.Path {
			out = append(out, s.NodeId+":"+s.Port)
		}
		return strings.Join(out, " ")
	}

	t.Run("full path to escalation with a preset choice", func(t *testing.T) {
		res := dry(&pb.DryRunTaskTypeDraftReq{
			SampleJson: [][]byte{sample("A1001", "10000001", time.Hour)},
			Choices:    map[string]string{"escalate": "handled"},
		})
		c := res.Cases[0]
		if !res.Passed || c.EndStatus != "succeeded" || len(c.Issues) != 0 {
			t.Fatalf("passed=%v end=%s issues=%+v", res.Passed, c.EndStatus, c.Issues)
		}
		if got := ports(c); !strings.Contains(got, "pending_48h:pending") || !strings.Contains(got, "escalate:handled") || !strings.Contains(got, "escalated:") {
			t.Fatalf("path = %s", got)
		}
		var targets []string
		for _, s := range c.Sends {
			targets = append(targets, s.NodeId+"→"+s.Target)
		}
		if strings.Join(targets, ",") != "notify_rm→rm.alice@example.com,remind_rm→rm.alice@example.com,escalate→group:vip_cs_leads" {
			t.Fatalf("sends = %v", targets)
		}
		var card struct{ Title string }
		if err := json.Unmarshal(c.Sends[2].RenderedJson, &card); err != nil || !strings.Contains(card.Title, "超过 7 天") {
			t.Fatalf("escalation card = %s", c.Sends[2].RenderedJson)
		}
		// 两个定时器都锚定在提交时间上：提交 1 小时后开始，分别在约 47 小时与 7 天差 1 小时到期。
		if len(c.Timeline) != 2 || c.Timeline[0].NodeId != "wait_48h" || c.Timeline[1].NodeId != "wait_7d" {
			t.Fatalf("timeline = %+v", c.Timeline)
		}
		if h := time.Duration(c.Timeline[0].OffsetMs) * time.Millisecond; h < 46*time.Hour || h > 48*time.Hour {
			t.Fatalf("wait_48h offset = %s", h)
		}
		if c.AssigneeId != "group:vip_cs_leads" {
			t.Fatalf("assignee = %s", c.AssigneeId)
		}
	})

	t.Run("stops at the human node without a choice; resolved appeal ends early", func(t *testing.T) {
		res := dry(&pb.DryRunTaskTypeDraftReq{SampleJson: [][]byte{
			sample("A1003", "10000003", 8*24*time.Hour),
			sample("A1004", "10000004", 3*24*time.Hour), // mock 库中已审核通过
		}})
		if !res.Passed || res.Cases[0].EndStatus != "waiting" || res.Cases[1].EndStatus != "succeeded" {
			t.Fatalf("passed=%v ends=%s/%s", res.Passed, res.Cases[0].EndStatus, res.Cases[1].EndStatus)
		}
		if got := ports(res.Cases[1]); !strings.Contains(got, "pending_48h:default") || !strings.Contains(got, "done:") || strings.Contains(got, "remind_rm") {
			t.Fatalf("A1004 path = %s", got)
		}
	})

	t.Run("bad sample and rejected choice fail the dry run", func(t *testing.T) {
		res := dry(&pb.DryRunTaskTypeDraftReq{
			SampleJson: [][]byte{[]byte(`[1]`), sample("A1003", "10000003", 8*24*time.Hour)},
			Choices:    map[string]string{"escalate": "snooze"},
		})
		if res.Passed || res.Cases[0].Issues[0].Code != "bad_sample" || res.Cases[1].Issues[0].Code != "choice_rejected" {
			t.Fatalf("res = %+v", res)
		}
	})

	t.Run("errors", func(t *testing.T) {
		_, err := NewDryRunTaskTypeDraftLogic(ctx, env.Svc).DryRunTaskTypeDraft(&pb.DryRunTaskTypeDraftReq{Key: "vip_appeal_alert"})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("no samples: %v", err)
		}
		_, err = NewDryRunTaskTypeDraftLogic(ctx, env.Svc).DryRunTaskTypeDraft(&pb.DryRunTaskTypeDraftReq{Key: "nope", SampleJson: [][]byte{[]byte(`{}`)}})
		if status.Code(err) != codes.NotFound {
			t.Fatalf("unknown type: %v", err)
		}
	})

	t.Run("instances sharing the dry-run database are serialized", func(t *testing.T) {
		def, err := env.Svc.Plans.Load(context.Background(), "vip_appeal_alert", 1)
		if err != nil {
			t.Fatal(err)
		}
		// 另一个实例：自己的 Runner（进程内互斥不共享），同一个试运行库。
		other := dryrun.New(sqlx.NewMysql(env.DryRun), env.Svc.Actions, nodes.Default(), func(string) bool { return true })
		in := dryrun.Input{Def: def, Samples: []json.RawMessage{sample("A1001", "10000001", time.Hour)},
			Choices: map[string]string{"escalate": "handled"}, Now: time.Now()}
		var wg sync.WaitGroup
		reports := make([]*dryrun.Report, 4)
		errs := make([]error, 4)
		for i := range reports {
			r := env.Svc.DryRun
			if i%2 == 1 {
				r = other
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				reports[i], errs[i] = r.Run(context.Background(), in)
			}()
		}
		wg.Wait()
		for i := range reports {
			if errs[i] != nil {
				t.Fatalf("run %d: %v", i, errs[i])
			}
			if c := reports[i].Cases[0]; c.EndStatus != "succeeded" || len(c.Sends) != 3 || len(c.Issues) != 0 {
				t.Fatalf("run %d: end=%s sends=%d issues=%+v", i, c.EndStatus, len(c.Sends), c.Issues)
			}
		}
	})

	// 平台库没有因试运行多出任务或 run；试运行库的运行数据已清空。
	if n := count(t, env.Platform, "select count(*) from task"); n != tasksBefore {
		t.Fatalf("platform tasks %d → %d", tasksBefore, n)
	}
	if n := count(t, env.Platform, "select count(*) from wf_run"); n != runsBefore {
		t.Fatalf("platform runs %d → %d", runsBefore, n)
	}
	if n := count(t, env.DryRun, "select count(*) from wf_job") + count(t, env.DryRun, "select count(*) from wf_run"); n != 0 {
		t.Fatalf("dry-run database not cleaned: %d rows", n)
	}
}
