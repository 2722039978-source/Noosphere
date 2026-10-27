import type { ModelConfig } from '@/lib/api';

type Brand='qwen'|'deepseek'|'openai'|'anthropic'|'google'|'meta'|'mistral'|'unknown';
const BRANDS:Record<string,Brand>={qwen:'qwen',alibaba:'qwen',通义千问:'qwen',deepseek:'deepseek',深度求索:'deepseek',openai:'openai',anthropic:'anthropic',claude:'anthropic',google:'google',gemini:'google',meta:'meta',llama:'meta',mistral:'mistral'};
const LABELS:Record<Brand,string>={qwen:'Qwen',deepseek:'DeepSeek',openai:'OpenAI',anthropic:'Anthropic',google:'Google',meta:'Meta',mistral:'Mistral',unknown:'未知厂商'};
const ASSETS:Partial<Record<Brand,string>>={qwen:'qwen-color.svg',deepseek:'deepseek-color.svg',openai:'openai.svg',anthropic:'anthropic.svg',google:'gemini-color.svg',meta:'meta-color.svg',mistral:'mistral-color.svg'};
const normalize=(value:string)=>value.trim().toLowerCase().replace(/[\s_-]+/g,'');
const series=(name:string):Brand=>{
  const parts=name.toLowerCase().split('/');const n=parts[parts.length-1]||'';
  if(/^(deepseek|deep-seek)/.test(n))return 'deepseek';
  if(/^(qwen|qwq|qvq)/.test(n))return 'qwen';
  if(/^(claude)/.test(n))return 'anthropic';
  if(/^(gemini|gemma)/.test(n))return 'google';
  if(/^(llama|meta-llama)/.test(n))return 'meta';
  if(/^(mistral|mixtral|codestral|devstral)/.test(n))return 'mistral';
  if(/^(gpt[-.]|chatgpt|o[134](?:-|$))/i.test(n))return 'openai';
  return 'unknown';
};
/** Explicit manufacturer, structured metadata, known series, neutral fallback.
 * Provider protocol never supplies a brand on its own. */
export function resolveModelIdentity(model:ModelConfig,metadata?:{vendor?:string;manufacturer?:string;family?:string}){
 metadata=metadata??model.metadata;
 const explicit=model.vendor?.trim();const structured=metadata?.vendor?.trim()||metadata?.manufacturer?.trim();
 const configuredFamily=series(model.model_family||''),providerFamily=series(metadata?.family||'');
 const brand:Brand=explicit?BRANDS[normalize(explicit)]??'unknown':configuredFamily!=='unknown'?configuredFamily:structured?BRANDS[normalize(structured)]??'unknown':providerFamily!=='unknown'?providerFamily:series(model.model_name);
 const vendor=explicit||(configuredFamily!=='unknown'?LABELS[brand]:structured)||LABELS[brand];
 let deployment:'local'|'api'|'unknown'='unknown';
 if(model.deployment==='local'||model.deployment==='api')deployment=model.deployment;
 else try{const host=new URL(model.base_url).hostname.toLowerCase();deployment=['localhost','127.0.0.1','[::1]','::1'].includes(host)?'local':'api';}catch{/* No endpoint assumption. */}
 const provider=({ollama:'Ollama',deepseek:'DeepSeek',openai:'OpenAI',openai_compatible:'OpenAI-compatible',claude:'Anthropic',anthropic:'Anthropic',gemini:'Gemini'} as Record<string,string>)[model.provider]||model.provider;
 const family=model.model_family||metadata?.family||(brand==='unknown'?'未提供':brand==='anthropic'?'Claude':brand==='google'?(/^gemma/i.test(model.model_name)?'Gemma':'Gemini'):brand==='meta'?'Llama':LABELS[brand]);
 return {brand,vendor,family,provider,deployment,label:`${deployment==='local'?'本地':deployment==='api'?'API':'未知部署'} · ${provider}`,icon:ASSETS[brand]?`/model-brands/${ASSETS[brand]}`:null};
}
