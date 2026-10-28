import { FormEvent, useEffect, useState } from 'react';

type APIError={error?:{message?:string}};
async function api<T>(url:string, init?:RequestInit):Promise<T>{
  const response=await fetch(url,{credentials:'same-origin',cache:'no-store',...init});
  if(!response.ok){const body=await response.json().catch(()=>({})) as APIError; throw new Error(body.error?.message||'暂时无法完成请求，请稍后重试');}
  if(response.status===204)return undefined as T;
  return response.json() as Promise<T>;
}

export function Registration({onRegistered,onBack}:{onRegistered:()=>void;onBack:()=>void}){
  const [notice,setNotice]=useState(''); const [busy,setBusy]=useState(false);
  async function submit(event:FormEvent<HTMLFormElement>){event.preventDefault();const form=new FormData(event.currentTarget);setBusy(true);setNotice('');
    try{await api('/api/register',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({code:form.get('code'),username:form.get('username'),password:form.get('password')})});onRegistered();}
    catch(e){setNotice((e as Error).message);}finally{setBusy(false);}}
  return <section className="login-card"><p className="eyebrow">CampusClaw · 账号注册</p><h1>使用邀请码注册</h1><p className="muted">请向本班教师获取邀请码。账号类型和班级由邀请码确定，注册时不能自行选择。</p><form onSubmit={submit}><label>教师提供的邀请码<input name="code" autoComplete="one-time-code" minLength={64} maxLength={64} required /></label><label>用户名<input name="username" autoComplete="username" maxLength={191} required /></label><label>密码<input name="password" type="password" autoComplete="new-password" minLength={12} maxLength={72} required /></label><button disabled={busy}>{busy?'正在注册…':'注册并登录'}</button></form>{notice&&<p className="notice" role="alert">{notice}</p>}<button className="text-button" onClick={onBack}>返回登录</button></section>;
}

type Invitation={id:number;role:'teacher'|'student';created_at:string;expires_at:string;revoked:boolean;consumed:boolean};
export function AccountInvitations(){
  const [items,setItems]=useState<Invitation[]>([]);const [role,setRole]=useState<'teacher'|'student'>('student');const [code,setCode]=useState('');const [notice,setNotice]=useState('');const [busy,setBusy]=useState(false);
  async function refresh(){try{setItems((await api<{items:Invitation[]}>('/api/teacher/invitations')).items);setNotice('');}catch(e){setNotice((e as Error).message);}}
  useEffect(()=>{void refresh();},[]);
  async function issue(){setBusy(true);setNotice('');setCode('');try{const result=await api<{code:string}>('/api/teacher/invitations',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({role})});setCode(result.code);await refresh();}catch(e){setNotice((e as Error).message);}finally{setBusy(false);}}
  async function revoke(id:number){setBusy(true);setNotice('');try{await api(`/api/teacher/invitations/${id}`,{method:'DELETE'});await refresh();}catch(e){setNotice((e as Error).message);}finally{setBusy(false);}}
  return <><section className="page-heading"><p className="eyebrow">教师 · 本班账号管理</p><h1>邀请码</h1><p className="muted">邀请码固定绑定你的班级和指定身份，24小时有效，只能使用一次。</p></section>{notice&&<p className="notice" role="alert">{notice}</p>}{code&&<section className="detail-card"><h2>请现在复制并发给对应同学</h2><p className="muted">这串码只显示这一次。离开页面后不能再次查看；如未复制，请撤销后重新签发。</p><pre className="content" aria-label="新邀请码">{code}</pre><button className="text-button" onClick={()=>setCode('')}>我已保存，隐藏邀请码</button></section>}<section className="search-card"><h2>签发新邀请码</h2><form onSubmit={e=>{e.preventDefault();void issue();}}><label>邀请码对应身份<select value={role} onChange={e=>setRole(e.target.value as 'teacher'|'student')}><option value="student">学生</option><option value="teacher">教师</option></select></label><button disabled={busy}>{busy?'正在签发…':'签发一次性邀请码'}</button></form></section><section className="list-card"><div className="list-title"><h2>本班邀请码记录</h2><button className="text-button" onClick={()=>void refresh()}>刷新</button></div>{items.length===0?<p className="empty">还没有邀请码</p>:<ul>{items.map(item=><li key={item.id} className="invitation-row"><div><strong>{item.role==='teacher'?'教师码':'学生码'}</strong><span>{new Date(item.created_at).toLocaleString()} 签发 · {new Date(item.expires_at).toLocaleString()} 到期</span><span>{item.consumed?'已使用':item.revoked?'已撤销':new Date(item.expires_at).getTime()<Date.now()?'已过期':'未使用'}</span></div>{!item.consumed&&!item.revoked&&new Date(item.expires_at).getTime()>Date.now()&&<button className="text-button" disabled={busy} onClick={()=>void revoke(item.id)}>撤销</button>}</li>)}</ul>}</section></>;
}
