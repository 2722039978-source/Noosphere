import {useState} from 'react';
import {Box} from 'lucide-react';
import type {ModelConfig} from '@/lib/api';
import {resolveModelIdentity} from './model-identity';
export function ModelIdentityIcon({model}:{model:ModelConfig}){
 const identity=resolveModelIdentity(model),[failed,setFailed]=useState('');
 return identity.icon&&failed!==identity.icon?<img className="ns-model-brand" src={identity.icon} alt={`${identity.vendor} 标识`} onError={()=>setFailed(identity.icon!)}/>:<Box size={25} strokeWidth={1.5} aria-label={identity.vendor==='未知厂商'?'通用模型标识':identity.vendor}/>;
}
