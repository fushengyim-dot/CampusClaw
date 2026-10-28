import { FormEvent, useCallback, useEffect, useState } from 'react';

type Tool = { name:string; description:string; input_schema:unknown; schema_hash:string };
type Server = { id:number; name:string; endpoint:string; token_configured:boolean; connected:boolean; last_error?:string; discovered_at?:string; tools:Tool[] };
type Skill = { id:number; name:string; enabled:boolean };
type Call = { id:number; server_name:string; tool_name:string; status:string; summary:string; conversation_id:number; turn_number:number; occurred_at:string };
const builtinEndpoint = 'http://mcp-tools:8091/mcp';

async function api<T>(url:string, init?:RequestInit):Promise<T>{
  const response=await fetch(url,{credentials:'same-origin',cache:'no-store',...init});
  if(!response.ok){const body=await response.json().catch(()=>({})) as {error?:{message?:string}};throw new Error(body.error?.message||'暂时无法完成请求。');}
  return response.json() as Promise<T>;
}

export function MCPAdmin(){
  const [servers,setServers]=useState<Server[]>([]);const [skills,setSkills]=useState<Skill[]>([]);const [calls,setCalls]=useState<Call[]>([]);const [skillID,setSkillID]=useState(0);const [references,setReferences]=useState<string[]>([]);const [name,setName]=useState('');const [endpoint,setEndpoint]=useState(builtinEndpoint);const [token,setToken]=useState('');const [busy,setBusy]=useState(false);const [notice,setNotice]=useState('');
  const refresh=useCallback(async()=>{try{const [serverResult,skillResult,callResult]=await Promise.all([api<{servers:Server[]}>('/api/mcp/servers'),api<{items:Skill[]}>('/api/skills'),api<{items:Call[]}>('/api/mcp/calls')]);setServers(serverResult.servers);setSkills(skillResult.items);setCalls(callResult.items);setNotice('');}catch(e){setNotice((e as Error).message);}},[]);
  useEffect(()=>{void refresh();},[refresh]);
  useEffect(()=>{if(!skillID&&skills.length)setSkillID(skills[0].id);},[skillID,skills]);
  useEffect(()=>{if(!skillID){setReferences([]);return;}let alive=true;api<{references:string[]}>(`/api/skills/${skillID}/mcp-tools`).then(x=>{if(alive)setReferences(x.references);}).catch(()=>{if(alive)setReferences([]);});return()=>{alive=false};},[skillID]);
  async function add(event:FormEvent<HTMLFormElement>){event.preventDefault();setBusy(true);setNotice('');const oneTimeToken=token;setToken('');try{await api('/api/mcp/servers',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name,endpoint,token:oneTimeToken})});setName('');setEndpoint(builtinEndpoint);await refresh();setNotice('服务器已保存。令牌不会显示在页面或接口返回中。');}catch(e){setNotice((e as Error).message);}finally{setBusy(false);}}
  async function action(id:number,kind:'connect'|'disconnect'|'delete'){setBusy(true);setNotice('');try{await api(`/api/mcp/servers/${id}${kind==='delete'?'':`/${kind}`}`,{method:kind==='delete'?'DELETE':'POST',headers:{'Content-Type':'application/json'},...(kind==='delete'?{}:{body:'{}'})});await refresh();setNotice(kind==='connect'?'已完成连接和工具清单更新。':kind==='disconnect'?'已断开连接并清空可用工具清单。':'服务器登记已删除。');}catch(e){setNotice((e as Error).message);await refresh();}finally{setBusy(false);}}
  async function saveBindings(){setBusy(true);setNotice('');try{const result=await api<{references:string[]}>(`/api/skills/${skillID}/mcp-tools`,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({references})});setReferences(result.references);setNotice('技能工具绑定已保存。之后只有匹配到这个已启用技能时，模型才会看到这些工具。');}catch(e){setNotice((e as Error).message);}finally{setBusy(false);}}
  function toggleReference(reference:string){setReferences(current=>current.includes(reference)?current.filter(x=>x!==reference):[...current,reference]);}
  return <section className="page-heading"><p className="eyebrow">教师管理 · 第6课</p><h1>MCP 工具服务器</h1><p className="muted">MCP 是一套让平台发现和调用工具的通信约定。这里管理本班工具服务；令牌只在服务端保存。</p>
    {notice&&<p role="status" className="search-notice">{notice}</p>}
    <section className="upload-card"><h2>本班服务器</h2>{servers.length===0&&<p className="muted">目前还没有登记服务器。项目内置的本地只读工具服务会在数据库检查后自动显示。</p>}
      {servers.map(server=><article key={server.id} className="search-result"><h3>{server.name}</h3><p>状态：{server.connected?'已连接':'未连接'} · 令牌：{server.token_configured?'已配置':'未配置'}</p><p className="muted">{server.endpoint}</p>{server.last_error&&<p role="status">最近失败：{server.last_error}</p>}
        <div className="button-row"><button disabled={busy||!server.token_configured} onClick={()=>void action(server.id,'connect')}>连接并更新工具</button><button disabled={busy||!server.connected} onClick={()=>void action(server.id,'disconnect')}>断开</button><button disabled={busy} onClick={()=>void action(server.id,'delete')}>删除登记</button></div>
        <h4>当前发现的工具（{server.tools.length}）</h4>{server.tools.length===0?<p className="muted">没有可调用的工具。断开后旧清单会被删除。</p>:<ul>{server.tools.map(tool=><li key={tool.name}><strong>{tool.name}</strong><p>{tool.description}</p><code>输入结构已从服务器读取；编号 {tool.schema_hash.slice(0,12)}</code></li>)}</ul>}
      </article>)}
    </section>
    <section className="upload-card"><h2>登记 MCP 工具服务</h2><p className="muted">可使用项目内置的只读服务，或登记兼容 MCP 2025-11-25 Streamable HTTP 的外部 HTTPS 公网服务。外部服务器需要独立 Bearer token；私网地址、重定向和 OAuth-only/stdio 服务不支持。保存登记不会连接服务，需教师再点击“连接并更新工具”。</p><form onSubmit={add}><label>服务器名称<input value={name} onChange={e=>setName(e.target.value)} maxLength={64} required /></label><label>服务地址<input type="url" value={endpoint} onChange={e=>setEndpoint(e.target.value)} maxLength={512} placeholder="https://tools.example.org/mcp" required /></label><label>Bearer 令牌（外部服务必填；提交后清空）<input type="password" value={token} onChange={e=>setToken(e.target.value)} autoComplete="new-password" minLength={token?24:0} maxLength={512} /></label><button disabled={busy||!name.trim()||!endpoint.trim()||(endpoint!==builtinEndpoint&&token.length<24)||(token.length>0&&token.length<24)}>{busy?'正在保存…':'保存服务器'}</button></form></section>
    <section className="upload-card"><h2>为技能绑定已发现的工具</h2>{skills.length===0?<p className="muted">先到“会话”页面创建课程技能。</p>:<><label>选择技能<select value={skillID} onChange={e=>setSkillID(Number(e.target.value))}>{skills.map(skill=><option key={skill.id} value={skill.id}>{skill.name}{skill.enabled?'':'（已停用）'}</option>)}</select></label><fieldset><legend>可选工具</legend>{servers.filter(server=>server.connected).flatMap(server=>server.tools.map(tool=>({server,tool,reference:`${tool.name}@${server.name}`}))).map(item=><label className="check-label" key={item.reference}><input type="checkbox" checked={references.includes(item.reference)} onChange={()=>toggleReference(item.reference)} />{item.reference}<span className="muted"> · {item.tool.description}</span></label>)}</fieldset><p className="muted">保存时服务端会重新检查：工具必须属于本班已连接服务器的当前清单。</p><button disabled={busy||!skillID} onClick={()=>void saveBindings()}>保存技能绑定</button></>}</section>
    <section className="upload-card"><h2>最近工具调用记录</h2>{calls.length===0?<p className="muted">还没有可查看的工具调用记录。</p>:<ul>{calls.map(call=><li key={call.id}>{call.server_name} · {call.tool_name} · {call.status} · {call.summary}<br/><span className="muted">会话 {call.conversation_id} · 第 {call.turn_number} 轮 · {new Date(call.occurred_at).toLocaleString()}</span></li>)}</ul>}</section>
  </section>;
}
