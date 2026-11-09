import { Link } from 'react-router-dom';
import './first-evaluation.css';

export function FirstEvaluationPath({ stage }: { stage: 'prepare' | 'running' | 'report' }) {
  const current = stage === 'report' ? 3 : stage === 'running' ? 2 : 1;
  const steps = [
    { title: '连接与版本', detail: '复用已接入模型', to: '/model-hub' },
    { title: '环境与参数', detail: '检查并保存配置', to: '/model-lab' },
    { title: '执行任务', detail: '后台保存进度' },
    { title: '阅读结果', detail: '核对质量与证据', to: '/evaluation-reports' },
    { title: '使用模型', detail: '明确选择会话模型', to: '/model-hub' },
  ];
  return <nav className="ns-first-path" aria-label="首次模型测评流程">{steps.map((step, index) => <div key={step.title} data-current={index === current} aria-current={index === current ? 'step' : undefined}><span>{String(index + 1).padStart(2, '0')}</span><div>{step.to && index !== current ? <Link to={step.to}>{step.title}</Link> : <strong>{step.title}</strong>}<small>{step.detail}</small></div></div>)}</nav>;
}
