import { NavLink } from 'react-router-dom';
import { Grid2X2, FlaskConical, TableProperties, ScanFace } from 'lucide-react';
import { MODEL_SECTIONS } from '@/app/model-navigation';
const ICONS = [Grid2X2, FlaskConical, TableProperties, ScanFace];
export function ModelSectionNav({variant='lab'}:{variant?:'hub'|'lab'}) {
 return <nav aria-label="模型档案导航" className={variant === 'hub' ? 'ns-hub-tabs' : 'ns-lab-tabs'}>{MODEL_SECTIONS.map((item,index)=>{
  const Icon=ICONS[index];return <NavLink key={item.id} to={item.to} end={item.id==='hub'}>{variant==='hub'&&<Icon size={18}/>} {item.label}</NavLink>;
 })}</nav>;
}
