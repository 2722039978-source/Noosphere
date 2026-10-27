export const MODEL_SECTIONS = [
 {id:'hub',to:'/model-hub',label:'Model Hub'},
 {id:'lab',to:'/model-lab',label:'Model Lab'},
 {id:'reports',to:'/evaluation-reports',label:'Reports'},
 {id:'releases',to:'/release-center',label:'Release Center'},
] as const;

export function modelSection(path: string) {
 return MODEL_SECTIONS.find(item => path === item.to || path.startsWith(item.to + '/'));
}
export function primaryNavigationPath(path: string) {
 return path === '/' || modelSection(path) ? '/model-hub' : path;
}
export function buildPrimaryNavigation(modules: readonly {to:string;name:string;english:string;number:string}[]) {
 return [
  {to:'/home',label:'工作台',english:'OVERVIEW',number:'00',end:true},
  ...modules.map(item=>({to:item.to,label:item.name,english:item.english,number:item.number,end:true})),
  {to:'/model-hub',label:'模型档案',english:'MODEL ARCHIVE',number:'07',end:true},
  {to:'/settings',label:'设置',english:'PREFERENCES',number:'06',end:true},
 ];
}
