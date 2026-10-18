// Command devgw 是仅供本地开发的网关：把前端的 HTTP POST + JSON 转发为对 app/rpc 的 gRPC 调用，
// 并把请求头 X-Dev-User 写入 gRPC metadata，模拟公司 gateway 的协议转换与身份注入。
// 平台本身不提供 HTTP 服务，线上由公司 gateway 完成同样的事；接入 gateway 后前端只需更换 base URL。
//
//	go run ./cmd/devgw                     # 在仓库根目录运行，默认监听 127.0.0.1:8088，转发到 127.0.0.1:9090
//
// 路由取自 app/rpc/workbench.proto 中每个方法行尾注释的路径，如 POST /byai_platform/api/v1/task/get。
// 字段名为 proto 中的 snake_case；int64 输出为字符串、bytes 输出为 base64（proto3 JSON 映射）。
// 错误返回 {"code": "<gRPC 状态码名>", "message": "..."}，HTTP 状态码与 grpc-gateway 的默认映射一致。
//
// 另有开发专用的 GET /dev/users：列出 mock 中的人员、所属组与接口级权限，供前端的「切换身份」使用。
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"go.yaml.in/yaml/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8088", "HTTP listen address")
	target := flag.String("target", "127.0.0.1:9090", "gRPC server address (app/rpc)")
	protoFile := flag.String("proto", "app/rpc/workbench.proto", "proto file with the HTTP paths")
	relations := flag.String("relations", "deploy/mock/vip_rm.yaml", "mock relations, for /dev/users")
	groups := flag.String("groups", "deploy/mock/groups.yaml", "mock groups, for /dev/users")
	permissions := flag.String("permissions", "deploy/mock/permissions.yaml", "mock permissions, for /dev/users")
	flag.Parse()

	routes, err := loadRoutes(*protoFile)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := grpc.NewClient(*target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	users, err := mockUsers(*relations, *groups, *permissions)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/byai_platform/", &gateway{conn: conn, routes: routes, timeout: 30 * time.Second})
	mux.HandleFunc("GET /dev/users", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"users": users})
	})
	log.Printf("devgw: %d routes, http://%s → grpc %s", len(routes), *listen, *target)
	log.Fatal(http.ListenAndServe(*listen, cors(mux)))
}

// devUser 是 /dev/users 中的一个身份。
type devUser struct {
	ID          string   `json:"id"`
	Roles       []string `json:"roles"`       // rm：业务关系表中的处理人
	Groups      []string `json:"groups"`      // 所属用户组
	Permissions []string `json:"permissions"` // 接口级权限，如 task.admin、run.admin
}

// mockUsers 从 mock 关系表、用户组与权限表中收集人员：关系表中的处理人标为 rm，组成员带上组名，并附上权限。
func mockUsers(relationsFile, groupsFile, permissionsFile string) ([]devUser, error) {
	byID := map[string]*devUser{}
	get := func(id string) *devUser {
		if byID[id] == nil {
			byID[id] = &devUser{ID: id, Roles: []string{}, Groups: []string{}, Permissions: []string{}}
		}
		return byID[id]
	}
	var rel map[string]map[string]string
	if err := readYAML(relationsFile, &rel); err != nil {
		return nil, err
	}
	for _, m := range rel {
		for _, who := range m {
			if u := get(who); len(u.Roles) == 0 {
				u.Roles = append(u.Roles, "rm")
			}
		}
	}
	var grp map[string][]string
	if err := readYAML(groupsFile, &grp); err != nil {
		return nil, err
	}
	for g, members := range grp {
		for _, who := range members {
			u := get(who)
			u.Groups = append(u.Groups, g)
		}
	}
	var perms map[string][]string
	if err := readYAML(permissionsFile, &perms); err != nil {
		return nil, err
	}
	for who, p := range perms {
		u := get(who)
		u.Permissions = append(u.Permissions, p...)
	}
	out := make([]devUser, 0, len(byID))
	for _, u := range byID {
		sort.Strings(u.Groups)
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func readYAML(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(raw, v)
}
