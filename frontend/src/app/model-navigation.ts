export const MODEL_SECTIONS = [
 {id:'hub',to:'/model-hub',label:'Model Hub'},
 {id:'lab',to:'/model-lab',label:'Model Lab'},
 {id:'reports',to:'/evaluation-reports',label:'Reports'},
 {id:'releases',to:'/release-center',label:'Release Center'},
] as const;

export function modelSection(path: string) {
 return MODEL_SECTIONS.find(item => path === item.to || path.startsWith(item.to + '/'));
}
