import { useEffect, useState } from 'react';
const apps = [
  { key:'lecture', title:'讲义问答', text:'围绕教师绑定的本班课程讲义回答问题，并显示切片出处。' },
  { key:'guided', title:'解题引导', text:'结合解题技能和本班资料，一步一步提示下一步怎么做。' },
  { key:'review', title:'复习整理', text:'按课次和主题整理核心概念、关联、易错点与自测问题。' },
];

export function Applications({ onOpen }:{onOpen:(key:string)=>void}) {
  const [enabled,setEnabled]=useState<string[]>([]);
  useEffect(()=>{fetch('/api/apps',{credentials:'same-origin',cache:'no-store'}).then(r=>r.ok?r.json():Promise.reject()).then((x:{items:{key:string;enabled:boolean}[]})=>setEnabled(x.items.filter(i=>i.enabled).map(i=>i.key))).catch(()=>setEnabled([]));},[]);
  return <><section className="page-heading"><p className="eyebrow">CampusClaw · 学习应用</p><h1>选择一个课程应用</h1><p className="muted">三个入口使用各自的本班智能体；每次回答仍只查看该助手绑定的资料。</p></section><section className="app-cards">{apps.map(app=><article className="app-card" key={app.key}><p className="eyebrow">内置应用</p><h2>{app.title}</h2><p className="muted">{app.text}</p><button disabled={!enabled.includes(app.key)} onClick={()=>onOpen(app.key)}>{enabled.includes(app.key)?'打开应用':'教师暂未启用'}</button></article>)}</section></>;
}
