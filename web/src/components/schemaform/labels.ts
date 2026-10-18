// 节点配置表单的中文标签：按字段名取，字段说明用 schema 中的 description。
// schema（api/schema/task_type.schema.json）是唯一的结构契约；标签属于展示层，放在这里。

export const fieldLabels: Record<string, string> = {
  // 通用
  view: '卡片模板',
  assignment: '处理人',
  to: '接收人',
  timeout: '超时',
  input: '参数',
  inputs: '参数',
  // 条件分支
  branches: '分支',
  port: '出口',
  label: '显示文字',
  when: '条件',
  // 汇合
  mode: '汇合方式',
  // 定时
  after: '等待时长',
  anchor: '起算时间',
  // 人工选择
  options: '选项',
  key: 'key',
  style: '样式',
  required_fields: '必填字段',
  snooze: '推迟时长',
  form: '表单',
  fields: '字段',
  type: '类型',
  remind: '重复提醒',
  every: '间隔',
  max: '最多次数',
  // skill / LLM
  skill: 'Skill',
  version: '版本',
  prompt: '提示词模板',
  model: '模型',
  max_tokens: '最大 token 数',
  // 邮件与事件
  cc: '抄送',
  subject: '主题',
  body: '正文',
  event_type: '事件类型',
  payload: '事件内容',
  // 结束
  resolution: '处理结论',
  // 处理人
  resolvers: '候选人来源',
  filters: '过滤',
  strategy: '选择策略',
  exclude: '排除',
  fallback: '兜底',
  group: '用户组',
  relation: '业务关系',
  // subject 在处理人解析器中是关系的主体
  users: '人员',
  agreement: '跨部门协议',
  node: '人工节点',
  expr: '表达式',
  default: '默认模板',
  variants: '受众变体',
};

/** 同名字段在不同位置含义不同时，按「父字段.字段」覆盖。 */
export const pathLabels: Record<string, string> = {
  'resolvers.subject': '主体',
  'form.fields': '表单字段',
  'fields.type': '控件',
  'fields.options': '候选值',
  'fallback.strategy': '兜底策略',
};

export const enumLabels: Record<string, Record<string, string>> = {
  style: { primary: '主按钮', default: '默认', danger: '危险' },
  mode: { all: '全部到达（all）', any: '任一到达（any）' },
  strategy: { first: '第一个', least_load: '负载最少', round_robin: '轮询', claim: '组内认领' },
  type: {
    relation: '业务关系',
    user: '指定人员',
    group: '用户组',
    agreement: '跨部门协议',
    previous_assignee: '上一人工节点的处理人',
    expr: '表达式',
    text: '单行文本',
    textarea: '多行文本',
    select: '下拉选择',
    datetime: '日期时间',
  },
  filters: { active: '在职', not_on_leave: '未请假', not_maker: '不是发起人', within_capacity: '未超负载' },
};

export function labelOf(name: string, parent?: string): string {
  return (parent && pathLabels[`${parent}.${name}`]) || fieldLabels[name] || name;
}
