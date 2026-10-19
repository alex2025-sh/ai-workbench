// 定义页的文字化工具：时长、cron、表达式与模板的展示。
//
// 模板中的 {{ payload.x }} 显示为字段名的标记（悬停看原始表达式），读者不必对照 schema 才知道引用的是什么；
// 表达式用等宽字体原样显示，不做翻译（翻译不准比不翻译更误导）。
import { Tooltip, Typography } from 'antd';
import { createContext, useContext, type ReactNode } from 'react';

/** 字段 key → 显示名，来自 payload schema 的 title。 */
export const FieldTitles = createContext<Record<string, string>>({});

const units: Record<string, [number, string]> = {
  ms: [1, '毫秒'],
  s: [1000, '秒'],
  m: [60_000, '分钟'],
  h: [3_600_000, '小时'],
  d: [86_400_000, '天'],
  w: [604_800_000, '周'],
};

/** 「48h」→「48 小时」，「1h30m」→「1 小时 30 分钟」；无法识别时原样返回。 */
export function durationText(v: unknown): string {
  if (typeof v !== 'string') return String(v ?? '');
  const parts = [...v.matchAll(/(\d+(?:\.\d+)?)(ms|s|m|h|d|w)/g)];
  if (parts.length === 0 || parts.map((p) => p[0]).join('') !== v.replace(/\s/g, '')) return v;
  return parts.map((p) => `${p[1]} ${units[p[2]][1]}`).join(' ');
}

/** 常见的 5 段 cron 转成中文；不认识的写法返回空，由调用方只显示原文。 */
export function cronText(expr: string): string {
  const f = expr.trim().split(/\s+/);
  if (f.length !== 5) return '';
  const [min, hour, dom, mon, dow] = f;
  const daily = dom === '*' && mon === '*' && dow === '*';
  let m: RegExpMatchArray | null;
  if (daily && hour === '*' && (min === '*' || min === '*/1')) return '每分钟';
  if (daily && hour === '*' && (m = min.match(/^\*\/(\d+)$/))) return `每 ${m[1]} 分钟`;
  if (daily && hour === '*' && /^\d+$/.test(min)) return min === '0' ? '每小时整点' : `每小时第 ${min} 分`;
  if (daily && /^\d+$/.test(min) && (m = hour.match(/^\*\/(\d+)$/))) return `每 ${m[1]} 小时（第 ${min} 分）`;
  const at = /^\d+$/.test(min) && /^\d+$/.test(hour) ? `${hour.padStart(2, '0')}:${min.padStart(2, '0')}` : '';
  if (!at) return '';
  if (daily) return `每天 ${at}`;
  if (dom === '*' && mon === '*' && dow === '1-5') return `工作日 ${at}`;
  const week = ['日', '一', '二', '三', '四', '五', '六', '日'];
  if (dom === '*' && mon === '*' && /^[0-7]$/.test(dow)) return `每周${week[Number(dow)]} ${at}`;
  if (/^\d+$/.test(dom) && mon === '*' && dow === '*') return `每月 ${dom} 日 ${at}`;
  return '';
}

const codeStyle = {
  fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
  fontSize: 12,
  background: 'rgba(0, 0, 0, 0.04)',
  border: '1px solid rgba(0, 0, 0, 0.06)',
  borderRadius: 4,
  padding: '1px 6px',
  wordBreak: 'break-all',
  whiteSpace: 'pre-wrap',
} as const;

/** 表达式：等宽原样显示。 */
export function Expr({ children }: { children: string }) {
  return <span style={codeStyle}>{children}</span>;
}

/** 表达式引用的字段显示名：payload.x → schema 的 title，task.x、viewer.x 用固定名称；其余返回空。 */
function refName(expr: string, titles: Record<string, string>): string {
  const e = expr.trim();
  let m = e.match(/^payload\.([A-Za-z_][\w]*)$/);
  if (m) return titles[m[1]] ?? m[1];
  m = e.match(/^task\.(\w+)$/);
  if (m) return ({ id: '任务 ID', type: '任务类型', priority: '优先级', assignee: '处理人', group: '处理组' } as Record<string, string>)[m[1]] ?? '';
  m = e.match(/^nodes\.(\w+)\.output\.(\w+)$/);
  if (m) return `${m[1]} 的 ${m[2]}`;
  return '';
}

/** 字段引用的标记：浅蓝底，悬停显示原始表达式。 */
export function Token({ expr }: { expr: string }) {
  const titles = useContext(FieldTitles);
  const name = refName(expr, titles);
  return (
    <Tooltip title={<span style={{ fontFamily: codeStyle.fontFamily }}>{expr.trim()}</span>}>
      <span
        style={{
          display: 'inline-block',
          margin: '0 2px',
          padding: '0 6px',
          borderRadius: 4,
          lineHeight: '20px',
          fontSize: 12,
          color: '#0958d9',
          background: '#e6f4ff',
          border: '1px solid #bae0ff',
          fontFamily: name ? undefined : codeStyle.fontFamily,
          wordBreak: 'break-all',
        }}
      >
        {name || expr.trim()}
      </span>
    </Tooltip>
  );
}

/** 模板：普通文本原样显示，{{ 表达式 }} 显示为标记。 */
export function Tmpl({ text }: { text: string }) {
  const out: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(/\{\{([^}]*)\}\}/g)) {
    if (m.index! > last) out.push(text.slice(last, m.index));
    out.push(<Token key={m.index} expr={m[1]} />);
    last = m.index! + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return <span style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{out}</span>;
}

/** 字段名：显示名加上灰色的 key。 */
export function FieldName({ name, title }: { name: string; title?: string }) {
  return (
    <span>
      {title && title !== name ? <Typography.Text>{title} </Typography.Text> : null}
      <Typography.Text type="secondary" style={{ fontFamily: codeStyle.fontFamily, fontSize: 12 }}>
        {name}
      </Typography.Text>
    </span>
  );
}
