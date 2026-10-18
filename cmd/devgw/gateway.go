package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	_ "github.com/alex2025-sh/ai-workbench/app/rpc/pb" // 注册 workbench.proto 的描述符
)

// UserHeader 是开发环境的身份请求头，devgw 把它写入 gRPC metadata，模拟 gateway 的身份注入。
const UserHeader = "X-Dev-User"

// userMetadataKey 必须与 app/rpc/internal/identity.MetadataKey 一致（cmd 不能 import app/rpc/internal）。
const userMetadataKey = "x-user-id"

// route 是一条 HTTP 路径到 gRPC 方法的映射。
type route struct {
	Path   string // /byai_platform/api/v1/task/get
	Method string // /workbench.v1.TaskService/GetTask
	In     protoreflect.MessageType
	Out    protoreflect.MessageType
}

var (
	servicePattern = regexp.MustCompile(`^service\s+(\w+)\s*\{`)
	rpcPattern     = regexp.MustCompile(`^rpc\s+(\w+)\s*\(\s*([\w.]+)\s*\)\s*returns\s*\(\s*([\w.]+)\s*\)\s*;\s*//\s*POST\s+(\S+)`)
	packagePattern = regexp.MustCompile(`^package\s+([\w.]+)\s*;`)
)

// loadRoutes 按 proto 中每个方法行尾注释的路径建立路由表，与 proto 保持同一份来源。
// 消息类型从生成代码注册的描述符中查找，proto 与生成代码不一致时报错。
func loadRoutes(protoFile string) (map[string]route, error) {
	f, err := os.Open(protoFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	routes := map[string]route{}
	var pkg, service string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := packagePattern.FindStringSubmatch(line); m != nil {
			pkg = m[1]
		}
		if m := servicePattern.FindStringSubmatch(line); m != nil {
			service = m[1]
		}
		m := rpcPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name, path := m[1], m[4]
		full := protoreflect.FullName(pkg + "." + service)
		desc, err := protoregistry.GlobalFiles.FindDescriptorByName(full)
		if err != nil {
			return nil, fmt.Errorf("devgw: service %s: %w (regenerate app/rpc/pb?)", full, err)
		}
		md := desc.(protoreflect.ServiceDescriptor).Methods().ByName(protoreflect.Name(name))
		if md == nil {
			return nil, fmt.Errorf("devgw: method %s.%s not in generated code", full, name)
		}
		in, err := protoregistry.GlobalTypes.FindMessageByName(md.Input().FullName())
		if err != nil {
			return nil, err
		}
		out, err := protoregistry.GlobalTypes.FindMessageByName(md.Output().FullName())
		if err != nil {
			return nil, err
		}
		if _, dup := routes[path]; dup {
			return nil, fmt.Errorf("devgw: duplicate path %s", path)
		}
		routes[path] = route{Path: path, Method: fmt.Sprintf("/%s/%s", full, name), In: in, Out: out}
	}
	return routes, sc.Err()
}

// gateway 把 HTTP POST + JSON 转发为 gRPC 调用，模拟公司 gateway 的协议转换与身份注入。
type gateway struct {
	conn    grpc.ClientConnInterface
	routes  map[string]route
	timeout time.Duration
}

var (
	unmarshal = protojson.UnmarshalOptions{DiscardUnknown: false}
	// 字段名用 proto 中的 snake_case；输出全部字段（含零值），前端不必区分“缺省”与“零值”。
	marshal = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}
)

func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt, ok := g.routes[r.URL.Path]
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no route for "+r.URL.Path)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
		return
	}
	in := rt.In.New().Interface()
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := unmarshal.Unmarshal(body, in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), g.timeout)
	defer cancel()
	if u := r.Header.Get(UserHeader); u != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, userMetadataKey, u)
	}
	out := rt.Out.New().Interface()
	if err := g.conn.Invoke(ctx, rt.Method, in, out); err != nil {
		st := status.Convert(err)
		writeError(w, httpStatus(st.Code()), st.Code().String(), st.Message())
		return
	}
	raw, err := marshal.Marshal(out.(proto.Message))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// httpStatus 与 grpc-gateway 的默认映射一致。
func httpStatus(c codes.Code) int {
	switch c {
	case codes.OK:
		return http.StatusOK
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		return http.StatusBadRequest
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists, codes.Aborted:
		return http.StatusConflict
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.Canceled:
		return 499
	case codes.Unimplemented:
		return http.StatusNotImplemented
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

// writeError 输出 {"code": "NOT_FOUND", "message": "..."}，code 为 gRPC 状态码名。
func writeError(w http.ResponseWriter, httpCode int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": msg})
}

// cors 允许本地前端开发服务器直接访问（生产由公司 gateway 负责）。
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+UserHeader)
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
