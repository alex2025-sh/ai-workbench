// 流程操作的确认框：任务详情与列表卡片共用。表单字段在节点上、所有选项共用，必填项由选项声明。
// 被拒绝时按 code 处理：invalid_form 留在表单，其余（已被处理、过期、无权限）交给调用方刷新。
import { Alert, App as AntApp, Form, Input, Modal, Select, Typography } from 'antd';
import { useState } from 'react';
import { ApiError } from '../api/client';
import { runApi } from '../api/services';
import type { ActionOption, PendingAction } from '../api/types';

export function SignalModal(props: {
  runId: string;
  action: PendingAction;
  option: ActionOption;
  onClose: () => void;
  onAccepted: () => void;
  onStale: (reason: string) => void;
}) {
  const { action, option } = props;
  const { message } = AntApp.useApp();
  const [form] = Form.useForm<Record<string, string>>();
  const [submitting, setSubmitting] = useState(false);
  const [rejected, setRejected] = useState<string>();

  const submit = async () => {
    const values = await form.validateFields();
    const formValues: Record<string, string> = {};
    Object.entries(values).forEach(([k, v]) => {
      if (v != null && String(v).trim() !== '') formValues[k] = String(v);
    });
    setSubmitting(true);
    setRejected(undefined);
    try {
      const res = await runApi.signal({
        run_id: props.runId,
        node_id: action.node_id,
        iteration: action.iteration,
        option: option.key,
        form: formValues,
      });
      if (res.accepted) return props.onAccepted();
      switch (res.code) {
        case 'invalid_form':
          setRejected(res.reason);
          break;
        case 'unsupported':
          message.info(res.reason);
          break;
        case 'forbidden':
          message.error(res.reason);
          props.onStale(res.reason);
          break;
        default: // already_handled / stale / not_waiting：通常是在 Lark 上先处理了
          props.onStale(res.reason);
      }
    } catch (e) {
      setRejected(e instanceof ApiError ? `${e.code}: ${e.message}` : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal open title={`处理任务：${option.label}`} okText="确认提交" onOk={() => void submit()} onCancel={props.onClose} confirmLoading={submitting}>
      {rejected && <Alert type="error" showIcon message={rejected} style={{ marginBottom: 16 }} />}
      <Form form={form} layout="vertical">
        {action.form_fields.length === 0 && <Typography.Text type="secondary">确认选择「{option.label}」？</Typography.Text>}
        {action.form_fields.map((f) => (
          <Form.Item
            key={f.key}
            name={f.key}
            label={f.label}
            rules={option.required_fields.includes(f.key) ? [{ required: true, message: `请填写${f.label}` }] : []}
          >
            {f.type === 'select' ? (
              <Select allowClear options={f.options.map((o) => ({ value: o, label: o }))} />
            ) : f.type === 'textarea' ? (
              <Input.TextArea rows={3} />
            ) : (
              <Input />
            )}
          </Form.Item>
        ))}
      </Form>
    </Modal>
  );
}
