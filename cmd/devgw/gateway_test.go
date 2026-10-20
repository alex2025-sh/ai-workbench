package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
)

const protoFile = "../../app/rpc/workbench.proto"

// 每个方法都有路径，且路径与生成代码中的方法一一对应。
func TestRoutesCoverEveryMethod(t *testing.T) {
	routes, err := loadRoutes(protoFile)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := protoregistry.GlobalFiles.FindFileByPath("workbench.proto")
	if err != nil {
		t.Fatal(err)
	}
	methods := 0
	for i := 0; i < fd.Services().Len(); i++ {
		methods += fd.Services().Get(i).Methods().Len()
	}
	if len(routes) != methods {
		t.Fatalf("%d routes for %d methods", len(routes), methods)
	}
	rt := routes["/byai_platform/api/v1/task/get"]
	if rt.Method != "/workbench.v1.TaskService/GetTask" || rt.Out.Descriptor().FullName() != protoreflect.FullName("workbench.v1.TaskDetail") {
		t.Fatalf("task/get = %+v", rt)
	}
}

type fakeTasks struct {
	pb.UnimplementedTaskServiceServer
}

func (fakeTasks) GetTask(ctx context.Context, in *pb.TaskIdReq) (*pb.TaskDetail, error) {
	if in.Id == 404 {
		return nil, status.Error(codes.NotFound, "task 404 not found")
	}
	md, _ := metadata.FromIncomingContext(ctx)
	return &pb.TaskDetail{Task: &pb.TaskSummary{Id: in.Id, AssigneeId: strings.Join(md.Get(userMetadataKey), ",")}}, nil
}

func newGateway(t *testing.T) http.Handler {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	pb.RegisterTaskServiceServer(srv, fakeTasks{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	routes, err := loadRoutes(protoFile)
	if err != nil {
		t.Fatal(err)
	}
	return cors(&gateway{conn: conn, routes: routes, timeout: 5 * time.Second})
}

func call(h http.Handler, method, path, user, body string) (*httptest.ResponseRecorder, map[string]any) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != "" {
		req.Header.Set(UserHeader, user)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestForwarding(t *testing.T) {
	h := newGateway(t)
	const path = "/byai_platform/api/v1/task/get"

	rec, out := call(h, http.MethodPost, path, "rm.alice@example.com", `{"id":"7"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	task := out["task"].(map[string]any)
	// int64 为字符串、字段名为 snake_case、零值字段也输出；身份经 metadata 传给服务端。
	if task["id"] != "7" || task["assignee_id"] != "rm.alice@example.com" {
		t.Fatalf("task = %v", task)
	}
	if _, ok := out["team_members"]; !ok {
		t.Fatalf("snake_case / unpopulated fields missing: %v", out)
	}

	for _, c := range []struct {
		method, path, body string
		status             int
		code               string
	}{
		{http.MethodPost, path, `{"id":"404"}`, http.StatusNotFound, "NotFound"},
		{http.MethodPost, path, `{"id":`, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{http.MethodPost, path, `{"nope":1}`, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{http.MethodGet, path, ``, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED"},
		{http.MethodPost, "/byai_platform/api/v1/nope", `{}`, http.StatusNotFound, "NOT_FOUND"},
		{http.MethodPost, "/byai_platform/api/v1/task/list", `{}`, http.StatusNotImplemented, "Unimplemented"},
	} {
		rec, out := call(h, c.method, c.path, "u", c.body)
		if rec.Code != c.status || out["code"] != c.code {
			t.Errorf("%s %s %s: %d %v", c.method, c.path, c.body, rec.Code, out)
		}
	}

	if rec, _ := call(h, http.MethodOptions, path, "", ""); rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatalf("preflight: %d %v", rec.Code, rec.Header())
	}
}

func TestMockUsers(t *testing.T) {
	users, err := mockUsers("../../deploy/mock/vip_rm.yaml", "../../deploy/mock/teams.sql", "../../deploy/mock/permissions.yaml")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]devUser{}
	for _, u := range users {
		got[u.ID] = u
	}
	if u := got["rm.alice@example.com"]; len(u.Roles) != 1 || u.Roles[0] != "rm" {
		t.Fatalf("alice = %+v", u)
	}
	if u := got["lead@example.com"]; len(u.Groups) != 2 || !slices.Contains(u.Groups, "vip_review:assigner") {
		t.Fatalf("lead = %+v", u)
	}
	if u := got["ops@example.com"]; !slices.Contains(u.Permissions, "team.admin") {
		t.Fatalf("ops = %+v", u)
	}
}
