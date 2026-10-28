import { FormEvent, ReactNode, useCallback, useEffect, useState } from 'react';
import { Tutor } from './Tutor';
import { Applications } from './Applications';
import { MCPAdmin } from './MCPAdmin';
import { AccountInvitations, Registration } from './Registration';

type User = { id: number; username: string; role: 'teacher' | 'student'; class_id: number };
type Material = { id: number; title: string; original_filename: string; size_bytes: number; source_url: string | null; source_locator: string | null; created_at: string };
type Detail = Material & { content: string };
type Page = { items: Material[]; total: number; page: number; page_size: number };
type APIError = { error?: { code?: string; message?: string } };
type SearchHit = { chunk_id: number; material_id: number; title: string; chunk_index: number; char_start: number; char_end: number; excerpt: string; source_url: string | null; source_locator: string | null; score: number };
type SearchResponse = { items: SearchHit[]; total: number; mode: string };
type VectorProgress = { total: number; ready: number; pending: number; failed: number; processed: number };

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, { credentials: 'same-origin', cache: 'no-store', ...init });
  if (!response.ok) {
    const body = await response.json().catch(() => ({})) as APIError;
    const fallback = response.status === 401 ? '请先登录或重新登录' : '暂时无法完成请求，请稍后重试';
    throw Object.assign(new Error(body.error?.message || fallback), { status: response.status, code: body.error?.code });
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export function App() {
  const [user, setUser] = useState<User | null>(null);
  const [checking, setChecking] = useState(true);
  const [notice, setNotice] = useState('');
  const [page, setPage] = useState<Page | null>(null);
  const [detail, setDetail] = useState<Detail | null>(null);
  const [query, setQuery] = useState('');
  const [searchMode, setSearchMode] = useState<'keyword' | 'vector' | 'hybrid'>('keyword');
  const [searchHits, setSearchHits] = useState<SearchHit[] | null>(null);
  const [searchNotice, setSearchNotice] = useState('');
  const [vectorProgress, setVectorProgress] = useState<VectorProgress | null>(null);
  const [vectorNotice, setVectorNotice] = useState('');
  const [vectorBusy, setVectorBusy] = useState(false);
  const [askQuestion, setAskQuestion] = useState('');
  const [askMaxEvidenceChunks, setAskMaxEvidenceChunks] = useState(1);
  const [askHistory, setAskHistory] = useState<{ role: 'user' | 'assistant'; content: string }[]>([]);
  const [askAnswer, setAskAnswer] = useState('');
  const [askCitations, setAskCitations] = useState<SearchHit[]>([]);
  const [askEvidenceCount, setAskEvidenceCount] = useState<number | null>(null);
  const [askEvidenceLimit, setAskEvidenceLimit] = useState(1);
  const [askNotice, setAskNotice] = useState('');
  const [askBusy, setAskBusy] = useState(false);
  const [busy, setBusy] = useState(false);
  const [route, setRoute] = useState(window.location.pathname);

  const go = useCallback((next: string) => { const url=new URL(next,window.location.href); window.history.pushState({}, '', url); setRoute(url.pathname); setNotice(''); }, []);
  useEffect(() => { const pop = () => setRoute(window.location.pathname); window.addEventListener('popstate', pop); return () => window.removeEventListener('popstate', pop); }, []);

  const loadList = useCallback(async () => {
    try { setPage(await request<Page>('/api/materials')); setNotice(''); }
    catch (e) { const err = e as Error & { status?: number }; if (err.status === 401) { setUser(null); go('/login'); } else setNotice(err.message); }
  }, [go]);

  useEffect(() => {
    request<User>('/api/me').then((me) => { setUser(me); setChecking(false); if (window.location.pathname === '/login' || window.location.pathname === '/') go('/apps'); })
      .catch((e: Error & { status?: number }) => { setChecking(false); if (e.status === 401) { setUser(null); if (window.location.pathname !== '/login' && window.location.pathname !== '/register') go('/login'); } else setNotice(e.message); });
  }, [go]);

  useEffect(() => {
    if (!checking && user && (route === '/materials' || route === '/')) void loadList();
    if (!checking && user && route.startsWith('/materials/')) {
      const id = route.split('/')[2];
      setVectorProgress(null); setVectorNotice('');
      request<Detail>(`/api/materials/${encodeURIComponent(id)}`).then(setDetail).catch((e: Error & { status?: number }) => {
        if (e.status === 401) { setUser(null); go('/login'); } else setNotice(e.message);
      });
      request<VectorProgress>(`/api/materials/${encodeURIComponent(id)}/vector-index`).then(setVectorProgress).catch(() => setVectorNotice('暂时无法读取向量索引状态。'));
    }
  }, [checking, user, route, loadList, go]);

  async function login(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setNotice(''); const form = new FormData(event.currentTarget);
    try { const me = await request<User>('/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: form.get('username'), password: form.get('password') }) }); setUser(me); go('/apps'); }
    catch (e) { setNotice((e as Error).message); } finally { setBusy(false); }
  }

  async function logout() {
    setBusy(true);
    try { await request<void>('/api/logout', { method: 'POST' }); setUser(null); setPage(null); setDetail(null); setAskHistory([]); setAskAnswer(''); setAskCitations([]); go('/login'); }
    catch (e) { const err = e as Error & { status?: number }; if (err.status === 401) { setUser(null); go('/login'); } else setNotice(err.message); } finally { setBusy(false); }
  }

  async function upload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget; const data = new FormData(form); setBusy(true); setNotice('');
    try { await request<{ id: number }>('/api/materials', { method: 'POST', body: data }); form.reset(); await loadList(); setNotice('讲义已保存，可以在列表中打开查看。'); }
    catch (e) { const err = e as Error & { code?: string }; setNotice(err.code === 'UPLOAD_RESULT_UNKNOWN' ? '上传结果暂时查不清。请先刷新讲义列表确认是否已出现，先不要重复上传。' : err.message); } finally { setBusy(false); }
  }

  async function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setSearchNotice(''); setSearchHits(null);
    try {
      const result = await request<SearchResponse>('/api/search', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ q: query, mode: searchMode, limit: 10, offset: 0 }) });
      setSearchHits(result.items);
      if (result.items.length === 0) setSearchNotice('本班资料中没有找到匹配内容。');
      else if (searchMode !== 'keyword') setSearchNotice('已用所选检索方式查找本班资料；语义检索会把本次问题发送给课程嵌入服务。');
    } catch (e) { setSearchNotice((e as Error).message); }
  }

  async function indexVectors(materialID: number) {
    const approved = window.confirm('向量化会把这份讲义的切片发送到课程嵌入服务，可能消耗课程额度。每次最多处理10段，不会自动重试。现在开始吗？');
    if (!approved) return;
    setVectorBusy(true); setVectorNotice('正在处理本批讲义切片…');
    try {
      const progress = await request<VectorProgress>(`/api/materials/${materialID}/vector-index`, { method: 'POST' });
      setVectorProgress(progress);
      setVectorNotice(progress.pending > 0 ? `本次处理 ${progress.processed} 段，尚有 ${progress.pending} 段待处理。再次点击可继续。` : `向量索引已完成：${progress.ready}/${progress.total} 段。`);
    } catch (e) {
      setVectorNotice((e as Error).message);
      try { setVectorProgress(await request<VectorProgress>(`/api/materials/${materialID}/vector-index`)); } catch { /* keep the current progress visible */ }
    } finally { setVectorBusy(false); }
  }

  async function ask(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setAskBusy(true); setAskNotice(''); setAskAnswer(''); setAskCitations([]); setAskEvidenceCount(null);
    try {
      const result = await request<{ answer: string; citations: SearchHit[]; evidence_count: number; max_evidence_chunks: number }>('/api/ask', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ q: askQuestion, history: askHistory, max_evidence_chunks: askMaxEvidenceChunks }) });
      setAskAnswer(result.answer); setAskCitations(result.citations);
      setAskEvidenceCount(result.evidence_count); setAskEvidenceLimit(result.max_evidence_chunks);
      const turns: { role: 'user' | 'assistant'; content: string }[] = [{ role: 'user', content: askQuestion }, { role: 'assistant', content: result.answer }];
      setAskHistory((history) => [...history, ...turns].slice(-8));
    } catch (e) { setAskNotice((e as Error).message); }
    finally { setAskBusy(false); }
  }

  if (checking) return <Shell><p role="status">正在确认登录状态…</p></Shell>;
  if (!user && route==='/register') return <Shell><Registration onRegistered={()=>{request<User>('/api/me').then(me=>{setUser(me);go('/apps');}).catch(()=>{go('/login');setNotice('注册已完成，请使用新账号登录。');});}} onBack={()=>go('/login')} /></Shell>;
  if (!user) return <Shell><section className="login-card"><p className="eyebrow">CampusClaw · 软件开发课程</p><h1>登录学习平台</h1><p className="muted">使用本班账号进入课程平台。</p><form onSubmit={login}><label>用户名<input name="username" autoComplete="username" required /></label><label>密码<input name="password" type="password" autoComplete="current-password" required /></label><button disabled={busy}>{busy ? '正在登录…' : '登录'}</button></form><Status notice={notice} /><p className="muted">已有本班教师发的邀请码？ <a href="/register" onClick={e=>{e.preventDefault();go('/register');}}>前往注册</a></p></section></Shell>;

  if (route === '/apps') return <Shell user={user} onLogout={logout} busy={busy}><Applications onOpen={(key)=>go(`/tutor?app=${encodeURIComponent(key)}`)} /></Shell>;
  if (route === '/tutor') return <Shell user={user} onLogout={logout} busy={busy}><Tutor user={user} onOpenMaterial={(id) => { setDetail(null); go(`/materials/${id}`); }} /></Shell>;
  if (route === '/mcp') return <Shell user={user} onLogout={logout} busy={busy}>{user.role === 'teacher' ? <MCPAdmin /> : <Status notice="请求的资源不存在。" />}</Shell>;
  if (route === '/accounts') return <Shell user={user} onLogout={logout} busy={busy}>{user.role==='teacher'?<AccountInvitations/>:<Status notice="请求的资源不存在。"/>}</Shell>;

  if (route.startsWith('/materials/')) return <Shell user={user} onLogout={logout} busy={busy}>
    <button className="text-button" onClick={() => go('/materials')}>← 返回讲义列表</button><Status notice={notice} />
    <section className="detail-card">{detail ? <>
      <p className="eyebrow">本班讲义</p><h1>{detail.title}</h1><p className="muted">{detail.original_filename} · {Math.ceil(detail.size_bytes / 1024)} KB</p>
      {(detail.source_url || detail.source_locator) && <p className="source">来源：{detail.source_url && <a href={detail.source_url} target="_blank" rel="noreferrer">{detail.source_url}</a>} {detail.source_locator}</p>}
      <section className="vector-index-card"><h2>语义检索准备</h2><p className="muted">只有教师点击后才会把这份讲义的切片发送到课程嵌入服务，可能消耗额度。每次最多处理10段；相同内容会跳过。</p>
        {vectorProgress && <p role="status">已处理 {vectorProgress.ready}/{vectorProgress.total} 段 · 待处理 {vectorProgress.pending} 段 · 失败 {vectorProgress.failed} 段</p>}
        {vectorNotice && <p className="search-notice" role="status">{vectorNotice}</p>}
        {user.role === 'teacher' && <button disabled={vectorBusy || !vectorProgress || (vectorProgress.pending === 0 && vectorProgress.failed === 0)} onClick={() => void indexVectors(detail.id)}>{vectorBusy ? '正在处理…' : vectorProgress?.ready ? '继续生成向量（最多10段）' : '生成向量索引（最多10段）'}</button>}
      </section>
      <pre className="content">{detail.content}</pre><a className="button-link" href={`/api/materials/${detail.id}/download`}>下载原文件</a>
    </> : <p role="status">正在读取讲义…</p>}</section>
  </Shell>;

  return <Shell user={user} onLogout={logout} busy={busy}><section className="page-heading"><p className="eyebrow">{user.class_id === 1 ? 'A班' : user.class_id === 2 ? 'B班' : '我的班级'} · {user.role === 'teacher' ? '教师' : '学生'}</p><h1>课程讲义</h1><p className="muted">你只会看到自己班级的资料。</p></section><Status notice={notice} /><section className="search-card"><h2>在本班讲义中查找</h2><p className="muted">关键词检索只在本机数据库搜索；向量和混合检索会把你的问题发送给课程嵌入服务。</p><form onSubmit={search}><label>检索方式<select value={searchMode} onChange={(event) => setSearchMode(event.target.value as 'keyword' | 'vector' | 'hybrid')}><option value="keyword">关键词</option><option value="vector">语义向量</option><option value="hybrid">关键词 + 语义混合</option></select></label><label>输入课程问题或关键词<input value={query} onChange={(event) => setQuery(event.target.value)} maxLength={1000} required /></label><button disabled={busy || !query.trim()}>搜索讲义</button></form>{searchNotice && <p className="search-notice" role="status">{searchNotice}</p>}{searchHits && searchHits.length > 0 && <ol className="search-results">{searchHits.map((hit) => <li key={hit.chunk_id}><button className="material-link" onClick={() => { setDetail(null); go(`/materials/${hit.material_id}`); }}><strong>{hit.title} · 第{hit.chunk_index + 1}段</strong><span>原文字符位置 {hit.char_start}–{hit.char_end}{hit.source_locator ? ` · ${hit.source_locator}` : ''}</span></button><p>{preview(hit.excerpt)}</p>{hit.source_url && <a href={hit.source_url} target="_blank" rel="noreferrer">打开原始来源</a>}</li>)}</ol>}
    <section className="ask-card"><h2>根据讲义回答</h2><p className="muted">提问会先把问题交给课程嵌入服务检索，可能消耗额度；有命中时，最多按你选择的段数把资料交给课程对话服务。没有命中时不调用对话服务。默认只发送1段。</p><form onSubmit={ask}><label>本轮最多发送给课程对话服务的资料段数<select value={askMaxEvidenceChunks} onChange={(event) => setAskMaxEvidenceChunks(Number(event.target.value))}><option value={1}>1段（默认）</option><option value={2}>2段</option><option value={3}>3段</option><option value={4}>4段</option></select></label><label>你的问题<input value={askQuestion} onChange={(event) => setAskQuestion(event.target.value)} maxLength={1000} required /></label><button disabled={askBusy || !askQuestion.trim()}>{askBusy ? '正在检索并回答…' : '基于本班资料回答'}</button></form>{askNotice && <p className="search-notice" role="status">{askNotice}</p>}{askAnswer && <div className="answer"><h3>回答</h3><p>{askAnswer}</p>{askEvidenceCount !== null && <p className="muted">本轮实际交给课程对话服务的资料：{askEvidenceCount}段（上限{askEvidenceLimit}段）</p>}{askCitations.length > 0 && <><h3>出处</h3><ol className="citations">{askCitations.map((hit, index) => <li key={hit.chunk_id}><button className="material-link" onClick={() => { setDetail(null); go(`/materials/${hit.material_id}`); }}><strong>[{index + 1}] {hit.title} · 第{hit.chunk_index + 1}段</strong><span>原文字符位置 {hit.char_start}–{hit.char_end}{hit.source_locator ? ` · ${hit.source_locator}` : ''}</span></button><p>{preview(hit.excerpt)}</p></li>)}</ol></>}</div>}</section>
    </section><div className="layout">{user.role === 'teacher' && <section className="upload-card"><h2>上传讲义</h2><p className="muted">支持 UTF-8 的 .txt 和 .md，单个文件不超过 5 MiB。</p><form onSubmit={upload}><label>标题<input name="title" maxLength={200} required /></label><label>课程资料文件<input name="file" type="file" accept=".txt,.md,text/plain,text/markdown" required /></label><label>来源网址（可选）<input name="source_url" type="url" /></label><label>章节或位置（可选）<input name="source_locator" maxLength={200} /></label><button disabled={busy}>{busy ? '正在保存…' : '上传并保存'}</button></form></section>}
    <section className="list-card"><div className="list-title"><h2>本班资料</h2><button className="text-button" onClick={() => void loadList()}>刷新</button></div>{page ? page.items.length === 0 ? <p className="empty">暂无讲义</p> : <ul>{page.items.map((item) => <li key={item.id}><button className="material-link" onClick={() => { setDetail(null); go(`/materials/${item.id}`); }}><strong>{item.title}</strong><span>{item.original_filename} · {new Date(item.created_at).toLocaleString()}</span></button></li>)}</ul> : <p role="status">正在读取本班讲义…</p>}</section></div></Shell>;
}

function Shell({ children, user, onLogout, busy }: { children: ReactNode; user?: User; onLogout?: () => void; busy?: boolean }) {
    return <main className="app"><header><a href="/apps" className="brand" onClick={(e) => { e.preventDefault(); window.history.pushState({}, '', '/apps'); window.dispatchEvent(new PopStateEvent('popstate')); }}>CampusClaw</a><span>软件开发课程学习平台</span>{user && <nav className="top-nav"><a href="/apps" onClick={(e)=>{e.preventDefault();window.history.pushState({},'','/apps');window.dispatchEvent(new PopStateEvent('popstate'));}}>应用</a><a href="/materials" onClick={(e)=>{e.preventDefault();window.history.pushState({},'','/materials');window.dispatchEvent(new PopStateEvent('popstate'));}}>讲义</a><a href="/tutor" onClick={(e)=>{e.preventDefault();window.history.pushState({},'','/tutor');window.dispatchEvent(new PopStateEvent('popstate'));}}>会话</a>{user.role==='teacher'&&<><a href="/accounts" onClick={(e)=>{e.preventDefault();window.history.pushState({},'','/accounts');window.dispatchEvent(new PopStateEvent('popstate'));}}>账号管理</a><a href="/mcp" onClick={(e)=>{e.preventDefault();window.history.pushState({},'','/mcp');window.dispatchEvent(new PopStateEvent('popstate'));}}>工具接入</a></>}</nav>}{user && <div className="account"><span>{user.username}</span><button className="text-button" disabled={busy} onClick={onLogout}>退出登录</button></div>}</header>{children}<footer>提供三个课程应用、本班资料出处和智能体流式问答。</footer></main>;
}

function Status({ notice }: { notice: string }) { return notice ? <p className="notice" role="alert">{notice}</p> : null; }

function preview(text: string) { const chars = Array.from(text); return chars.length > 220 ? `${chars.slice(0, 220).join('')}…` : text; }
