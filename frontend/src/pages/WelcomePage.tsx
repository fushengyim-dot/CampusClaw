export function WelcomePage() {
  return (
    <main>
      <header><span className="brand">CampusClaw</span><span>软件开发课程学习平台</span></header>
      <section aria-labelledby="welcome-title">
        <p className="eyebrow">从一份讲义，开始理解一门课</p>
        <h1 id="welcome-title">我们的学习平台，<br />从这里开始。</h1>
        <p className="intro">老师分享课程资料，同学在本班范围内学习、提问和复习。</p>
        <aside aria-labelledby="progress-title">
          <h2 id="progress-title">当前进度 · 第 3 课第一步</h2>
          <p>你已打开项目的起始页。登录、班级资料和讲义上传还没有接入。</p>
          <p className="next">下一步：连接数据库，再逐步实现登录。</p>
        </aside>
      </section>
      <footer>先把基础做好，再逐步加入讲义问答、解题引导与复习整理。</footer>
    </main>
  );
}
