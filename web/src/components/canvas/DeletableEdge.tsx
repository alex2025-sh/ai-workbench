// 画布连线：选中后在中点显示删除按钮（Delete / Backspace 同样可删）。退回边只读，不显示按钮。
import { CloseOutlined } from '@ant-design/icons';
import { BaseEdge, EdgeLabelRenderer, getBezierPath, type EdgeProps } from '@xyflow/react';

export interface EdgeData extends Record<string, unknown> {
  onRemove?: (id: string) => void;
}

export function DeletableEdge(props: EdgeProps) {
  const { id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, selected, markerEnd, style, label, data } = props;
  const [path, labelX, labelY] = getBezierPath({ sourceX, sourceY, sourcePosition, targetX, targetY, targetPosition });
  const onRemove = (data as EdgeData | undefined)?.onRemove;
  return (
    <>
      <BaseEdge id={id} path={path} markerEnd={markerEnd} style={selected ? { ...style, stroke: '#1677ff', strokeWidth: 2.5 } : style} interactionWidth={24} />
      {(label || (selected && onRemove)) && (
        <EdgeLabelRenderer>
          <div
            className="nodrag nopan"
            style={{ position: 'absolute', transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`, pointerEvents: 'all', display: 'flex', alignItems: 'center', gap: 4 }}
          >
            {label && <span style={{ fontSize: 11, background: '#fff', padding: '0 4px', borderRadius: 3, color: '#fa8c16' }}>{label}</span>}
            {selected && onRemove && (
              <button
                title="删除连线"
                onClick={(ev) => {
                  ev.stopPropagation();
                  onRemove(id);
                }}
                style={{ width: 20, height: 20, padding: 0, borderRadius: '50%', border: '1px solid #ff4d4f', background: '#fff', color: '#ff4d4f', cursor: 'pointer', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 10 }}
              >
                <CloseOutlined />
              </button>
            )}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}
