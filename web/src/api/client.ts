// 统一的接口客户端：业务代码不接触原始编码。
//
// 约定（devgw 按 proto3 JSON 映射实现，公司 gateway 的实际行为待确认，有差异只改这里）：
//   - 一律 POST /byai_platform/api/v1/<资源>/<动作>，字段名为 proto 的 snake_case，零值字段也会返回；
//   - int64（id、run_id、时间戳）是字符串，前端按字符串处理；时间戳为 Unix 毫秒；
//   - bytes（*_json）是 base64 编码的 UTF-8 JSON，用 decodeJson / encodeJson 转换；
//   - 枚举是名字（TASK_STATUS_PENDING），不使用数字；
//   - 错误为 {code, message}，code 是 gRPC 状态码名。

const BASE = '/byai_platform/api/v1';

/** 接口错误，code 为 gRPC 状态码名，如 NotFound、Unauthenticated。 */
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

let currentUser = '';

/** 设置调用人（开发环境写入 X-Dev-User；接入 gateway 后由 gateway 认证注入）。 */
export function setUser(user: string) {
  currentUser = user;
}

export async function call<T>(path: string, body: unknown = {}): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (currentUser) headers['X-Dev-User'] = currentUser;
  const res = await fetch(`${BASE}/${path}`, { method: 'POST', headers, body: JSON.stringify(body) });
  const text = await res.text();
  let data: unknown = {};
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    throw new ApiError(res.status, 'BAD_RESPONSE', text.slice(0, 200));
  }
  if (!res.ok) {
    const e = data as { code?: string; message?: string };
    throw new ApiError(res.status, e.code ?? 'UNKNOWN', e.message ?? res.statusText);
  }
  return data as T;
}

/** 解码 bytes 字段（base64 的 UTF-8 JSON）；为空或解析失败时返回 null。 */
export function decodeJson<T = unknown>(b64: string | null | undefined): T | null {
  if (!b64) return null;
  try {
    const bin = atob(b64);
    const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
    return JSON.parse(new TextDecoder().decode(bytes)) as T;
  } catch {
    return null;
  }
}

/** 编码 bytes 字段：JSON → UTF-8 → base64。 */
export function encodeJson(v: unknown): string {
  return encodeText(JSON.stringify(v));
}

/** 把一段已经是 JSON 的文本编码为 bytes 字段（不重新序列化，保留大数字与金额字符串的原样）。 */
export function encodeText(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let bin = '';
  bytes.forEach((b) => (bin += String.fromCharCode(b)));
  return btoa(bin);
}

/** 开发环境的身份列表（cmd/devgw 的 GET /dev/users）。 */
export interface DevUser {
  id: string;
  roles: string[];
  groups: string[];
}

export async function listDevUsers(): Promise<DevUser[]> {
  const res = await fetch('/dev/users');
  if (!res.ok) throw new ApiError(res.status, 'UNAVAILABLE', 'devgw /dev/users unavailable');
  return ((await res.json()) as { users: DevUser[] }).users;
}
