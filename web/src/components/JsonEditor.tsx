// JSON 编辑器（CodeMirror，随项目打包、不依赖 CDN）：文本合法时才回传解析结果，不合法时就地提示、不覆盖数据。
import { json } from '@codemirror/lang-json';
import CodeMirror from '@uiw/react-codemirror';
import { Typography } from 'antd';
import { useEffect, useState } from 'react';

const extensions = [json()];

export function JsonEditor(props: { value: unknown; onChange: (v: unknown) => void; height?: string; readOnly?: boolean }) {
  const format = (v: unknown) => (v === undefined ? '' : JSON.stringify(v, null, 2));
  const [text, setText] = useState(() => format(props.value));
  const [error, setError] = useState<string>();

  // 外部数据变化（如表单里改了同一份配置）时同步文本；用户正在编辑的内容与外部一致时不打断。
  useEffect(() => {
    let same = false;
    try {
      same = JSON.stringify(text.trim() ? JSON.parse(text) : undefined) === JSON.stringify(props.value);
    } catch {
      same = false;
    }
    if (!same && !error) setText(format(props.value));
  }, [props.value]); // 只在外部值变化时同步，text / error 的变化不触发

  const change = (t: string) => {
    setText(t);
    if (!t.trim()) {
      setError(undefined);
      props.onChange(undefined);
      return;
    }
    try {
      props.onChange(JSON.parse(t));
      setError(undefined);
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return (
    <div>
      <CodeMirror value={text} height={props.height ?? '360px'} extensions={extensions} onChange={change} readOnly={props.readOnly} basicSetup={{ foldGutter: true }} />
      {error && (
        <Typography.Text type="danger" style={{ fontSize: 12 }}>
          JSON 不合法，未保存：{error}
        </Typography.Text>
      )}
    </div>
  );
}
