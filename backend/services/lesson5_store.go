package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"campusclaw/backend/repositories"
)

var ErrLesson5NotFound = errors.New("资源不存在")
var ErrLesson5Invalid = errors.New("请求内容不符合要求")
var ErrLesson5Unavailable = errors.New("第5课服务暂不可用")

type Lesson5Store struct{ db *sql.DB }

func NewLesson5Store(db *sql.DB) *Lesson5Store { return &Lesson5Store{db: db} }

// EnsureSeedSkills installs the two official A-class examples once. Existing
// rows, including teacher edits, are never overwritten.
func (s *Lesson5Store) EnsureSeedSkills(ctx context.Context) error {
	var classID int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM classes WHERE name='A班' ORDER BY id LIMIT 1`).Scan(&classID); err != nil {
		return ErrLesson5Unavailable
	}
	seeds := []struct{ key, raw string }{
		{"lesson5-math-guidance", "名称：数学解题引导\n适用条件：方程,去括号,分数,通分,面积,应用题,未知数,行程\n资料：数学\n工具：knowledge_search\n启用：是\n\n学生正在自己做数学作业，往往已经写了一步。先看懂他卡在哪一步，只指出下一步该检查什么，例如去括号有没有乘到每一项、分母不同要不要先通分、该设哪个未知数。不要算出最终得数，不要把整道题做完。依据只用数学自学笔记，并标出材料标题和位置。"},
		{"lesson5-chinese-guidance", "名称：语文解题引导\n适用条件：春晓,默写,阅读,中心,作文,开头,古诗,词语\n资料：语文\n工具：knowledge_search\n启用：是\n\n学生正在自己背诵、做阅读或改作文，卡在某一句。先接住他已经写出的那一句，告诉他往前看哪一句、找哪类句子。不要把诗的下一句、中心句或改好的段落直接写出来。依据只用语文自学笔记，并标出材料标题和位置。"},
	}
	for _, seed := range seeds {
		doc, err := ParseSkillDocument(seed.raw)
		if err != nil {
			return ErrLesson5Unavailable
		}
		tools, _ := json.Marshal(doc.Tools)
		if _, err = s.db.ExecContext(ctx, `INSERT INTO course_skills(class_id,name,trigger_text,subject_text,tools_json,enabled,description,source_text,seed_key) VALUES(?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE seed_key=VALUES(seed_key)`, classID, doc.Name, doc.TriggerText, doc.SubjectText, string(tools), doc.Enabled, doc.Description, doc.SourceText, seed.key); err != nil {
			return ErrLesson5Unavailable
		}
	}
	return nil
}

type BuiltinApp struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
	AgentID     int64  `json:"agent_id"`
	Enabled     bool   `json:"enabled"`
}

func (s *Lesson5Store) EnsureBuiltinAgents(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM classes ORDER BY id`)
	if err != nil {
		return fmt.Errorf("%w: list class rows: %v", ErrLesson5Unavailable, err)
	}
	classes := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) != nil {
			rows.Close()
			return fmt.Errorf("%w: read class row", ErrLesson5Unavailable)
		}
		classes = append(classes, id)
	}
	if rows.Err() != nil {
		rows.Close()
		return fmt.Errorf("%w: iterate class rows: %v", ErrLesson5Unavailable, rows.Err())
	}
	rows.Close()
	seeds := []struct{ key, name, prompt string }{
		{"lecture", "课程讲义问答助手", "你是软件开发课程讲义问答助手。先直接回答学生的问题，优先使用本班绑定讲义中的证据；表达清楚、简洁，并使用对应引用编号。资料不足时诚实说明不确定。"},
		{"guided", "课程解题引导助手", "你是软件开发课程解题引导助手。先理解学生已经尝试的步骤，再给出可执行的下一步提示；没有学生要求时不要一次性替他完成整道题。"},
		{"review", "课程复习整理助手", "你是软件开发课程复习整理助手。围绕学生指定的课次和主题，整理核心概念、概念之间的关系、易错点和简短自测问题；只对有资料支持的内容给出来源编号。"},
	}
	for _, classID := range classes {
		for _, seed := range seeds {
			tx, e := s.db.BeginTx(ctx, nil)
			if e != nil {
				return fmt.Errorf("%w: begin seed transaction: %v", ErrLesson5Unavailable, e)
			}
			var locked int64
			if e = tx.QueryRowContext(ctx, `SELECT id FROM classes WHERE id=? FOR UPDATE`, classID).Scan(&locked); e != nil {
				_ = tx.Rollback()
				return fmt.Errorf("%w: lock class row: %v", ErrLesson5Unavailable, e)
			}
			var agentID int64
			e = tx.QueryRowContext(ctx, `SELECT agent_id FROM course_builtin_agents WHERE class_id=? AND builtin_key=?`, classID, seed.key).Scan(&agentID)
			if e == nil {
				_ = tx.Rollback()
				continue
			}
			if !errors.Is(e, sql.ErrNoRows) {
				_ = tx.Rollback()
				return fmt.Errorf("%w: read built-in mapping: %v", ErrLesson5Unavailable, e)
			}
			e = tx.QueryRowContext(ctx, `SELECT id FROM course_agents WHERE class_id=? AND name=?`, classID, seed.name).Scan(&agentID)
			if errors.Is(e, sql.ErrNoRows) {
				res, insertErr := tx.ExecContext(ctx, `INSERT INTO course_agents(class_id,name,prompt,enabled) VALUES(?,?,?,1)`, classID, seed.name, seed.prompt)
				if insertErr != nil {
					_ = tx.Rollback()
					return fmt.Errorf("%w: insert built-in agent: %v", ErrLesson5Unavailable, insertErr)
				}
				agentID, _ = res.LastInsertId()
			} else if e != nil {
				_ = tx.Rollback()
				return fmt.Errorf("%w: find existing built-in agent: %v", ErrLesson5Unavailable, e)
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO course_builtin_agents(class_id,builtin_key,agent_id) VALUES(?,?,?)`, classID, seed.key, agentID); e != nil {
				_ = tx.Rollback()
				return fmt.Errorf("%w: insert built-in mapping: %v", ErrLesson5Unavailable, e)
			}
			if seed.key == "guided" {
				skillRows, queryErr := tx.QueryContext(ctx, `SELECT id FROM course_skills WHERE class_id=? AND seed_key IS NOT NULL ORDER BY id`, classID)
				if queryErr != nil {
					_ = tx.Rollback()
					return fmt.Errorf("%w: list seed skills: %v", ErrLesson5Unavailable, queryErr)
				}
				var skillIDs []int64
				for skillRows.Next() {
					var sid int64
					if skillRows.Scan(&sid) != nil {
						skillRows.Close()
						_ = tx.Rollback()
						return fmt.Errorf("%w: read seed skill", ErrLesson5Unavailable)
					}
					skillIDs = append(skillIDs, sid)
				}
				if skillRows.Err() != nil {
					rowErr := skillRows.Err()
					skillRows.Close()
					_ = tx.Rollback()
					return fmt.Errorf("%w: iterate seed skills: %v", ErrLesson5Unavailable, rowErr)
				}
				skillRows.Close()
				for _, sid := range skillIDs {
					if _, e = tx.ExecContext(ctx, `INSERT INTO course_agent_skills(agent_id,skill_id,class_id) VALUES(?,?,?)`, agentID, sid, classID); e != nil {
						_ = tx.Rollback()
						return fmt.Errorf("%w: bind seed skill: %v", ErrLesson5Unavailable, e)
					}
				}
			}
			if e = tx.Commit(); e != nil {
				return fmt.Errorf("%w: commit built-in seed: %v", ErrLesson5Unavailable, e)
			}
		}
	}
	return nil
}
func (s *Lesson5Store) ListBuiltinApps(ctx context.Context, classID int64) ([]BuiltinApp, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT b.builtin_key,a.name,a.enabled,a.id FROM course_builtin_agents b JOIN course_agents a ON a.id=b.agent_id AND a.class_id=b.class_id WHERE b.class_id=? ORDER BY FIELD(b.builtin_key,'lecture','guided','review')`, classID)
	if err != nil {
		return nil, ErrLesson5Unavailable
	}
	defer rows.Close()
	description := map[string]string{"lecture": "根据本班讲义回答课程问题，并查看回答依据。", "guided": "结合课程资料分步骤提示实操或问题的下一步。", "review": "围绕课程范围整理知识要点、易错点和自测问题。"}
	titles := map[string]string{"lecture": "讲义问答", "guided": "解题引导", "review": "复习整理"}
	out := []BuiltinApp{}
	for rows.Next() {
		var x BuiltinApp
		if rows.Scan(&x.Key, &x.Title, &x.Enabled, &x.AgentID) != nil {
			return nil, ErrLesson5Unavailable
		}
		x.Title = titles[x.Key]
		x.Description = description[x.Key]
		out = append(out, x)
	}
	if rows.Err() != nil {
		return nil, ErrLesson5Unavailable
	}
	return out, nil
}

type Agent struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Prompt     string `json:"prompt"`
	Enabled    bool   `json:"enabled"`
	BuiltinKey string `json:"builtin_key,omitempty"`
}
type AgentConfig struct {
	Agent
	SkillIDs    []int64 `json:"skill_ids"`
	MaterialIDs []int64 `json:"material_ids"`
}
type StoredSkill struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	TriggerText string `json:"trigger_text"`
	SubjectText string `json:"subject_text"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}
type Conversation struct {
	ID        int64  `json:"id"`
	AgentID   int64  `json:"agent_id"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updated_at"`
}
type StoredMessage struct {
	ID        int64          `json:"id"`
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	Citations []KnowledgeHit `json:"citations"`
	SkillName *string        `json:"skill_name,omitempty"`
}

func (s *Lesson5Store) ListAgents(ctx context.Context, u repositories.User) ([]Agent, error) {
	q := `SELECT a.id,a.name,a.prompt,a.enabled,COALESCE(b.builtin_key,'') FROM course_agents a LEFT JOIN course_builtin_agents b ON b.agent_id=a.id AND b.class_id=a.class_id WHERE a.class_id=?`
	if u.Role != "teacher" {
		q += ` AND a.enabled=1`
	}
	q += ` ORDER BY a.id`
	rows, err := s.db.QueryContext(ctx, q, u.ClassID)
	if err != nil {
		return nil, ErrLesson5Unavailable
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		var a Agent
		if rows.Scan(&a.ID, &a.Name, &a.Prompt, &a.Enabled, &a.BuiltinKey) != nil {
			return nil, ErrLesson5Unavailable
		}
		out = append(out, a)
	}
	if rows.Err() != nil {
		return nil, ErrLesson5Unavailable
	}
	return out, nil
}

func (s *Lesson5Store) SaveAgent(ctx context.Context, u repositories.User, id int64, a Agent, skillIDs, materialIDs []int64) (int64, error) {
	if u.Role != "teacher" {
		return 0, ErrNotTeacher
	}
	if utf8.RuneCountInString(strings.TrimSpace(a.Name)) < 1 || utf8.RuneCountInString(a.Name) > 64 || utf8.RuneCountInString(a.Prompt) > 2000 || len(skillIDs) > 100 || len(materialIDs) > 100 {
		return 0, ErrLesson5Invalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, ErrLesson5Unavailable
	}
	defer tx.Rollback()
	if id > 0 {
		var exists int
		if tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM course_agents WHERE id=? AND class_id=?`, id, u.ClassID).Scan(&exists) != nil || exists != 1 {
			return 0, ErrLesson5NotFound
		}
		res, e := tx.ExecContext(ctx, `UPDATE course_agents SET name=?,prompt=?,enabled=? WHERE id=? AND class_id=?`, strings.TrimSpace(a.Name), a.Prompt, a.Enabled, id, u.ClassID)
		if e != nil {
			return 0, ErrLesson5Invalid
		}
		_ = res // existence was checked above; MySQL may report zero changed rows for an identical update.
	} else {
		res, e := tx.ExecContext(ctx, `INSERT INTO course_agents(class_id,name,prompt,enabled) VALUES(?,?,?,?)`, u.ClassID, strings.TrimSpace(a.Name), a.Prompt, a.Enabled)
		if e != nil {
			return 0, ErrLesson5Invalid
		}
		id64, _ := res.LastInsertId()
		id = id64
	}
	// Replace bindings atomically. The composite FKs reject cross-class IDs.
	if _, err = tx.ExecContext(ctx, `DELETE FROM course_agent_skills WHERE agent_id=? AND class_id=?`, id, u.ClassID); err != nil {
		return 0, ErrLesson5Unavailable
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM course_agent_materials WHERE agent_id=? AND class_id=?`, id, u.ClassID); err != nil {
		return 0, ErrLesson5Unavailable
	}
	for _, sid := range uniqueIDs(skillIDs) {
		res, e := tx.ExecContext(ctx, `INSERT INTO course_agent_skills(agent_id,skill_id,class_id) SELECT ?,id,class_id FROM course_skills WHERE id=? AND class_id=?`, id, sid, u.ClassID)
		if e != nil {
			return 0, ErrLesson5NotFound
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return 0, ErrLesson5NotFound
		}
	}
	for _, mid := range uniqueIDs(materialIDs) {
		res, e := tx.ExecContext(ctx, `INSERT INTO course_agent_materials(agent_id,material_id,class_id) SELECT ?,id,class_id FROM materials WHERE id=? AND class_id=?`, id, mid, u.ClassID)
		if e != nil {
			return 0, ErrLesson5NotFound
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return 0, ErrLesson5NotFound
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, ErrLesson5Unavailable
	}
	return id, nil
}

func uniqueIDs(ids []int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id < 1 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (s *Lesson5Store) ReadAgent(ctx context.Context, classID, id int64, student bool) (Agent, error) {
	var a Agent
	q := `SELECT a.id,a.name,a.prompt,a.enabled,COALESCE(b.builtin_key,'') FROM course_agents a LEFT JOIN course_builtin_agents b ON b.agent_id=a.id AND b.class_id=a.class_id WHERE a.id=? AND a.class_id=?`
	if student {
		q += ` AND a.enabled=1`
	}
	if s.db.QueryRowContext(ctx, q, id, classID).Scan(&a.ID, &a.Name, &a.Prompt, &a.Enabled, &a.BuiltinKey) != nil {
		return Agent{}, ErrLesson5NotFound
	}
	if a.Prompt == "" {
		a.Prompt = "你是软件开发课程的学习助手。用简洁中文帮助学生理解概念、分析问题并给出下一步建议。"
	}
	return a, nil
}

func (s *Lesson5Store) ReadAgentConfig(ctx context.Context, classID, id int64) (AgentConfig, error) {
	a, err := s.ReadAgent(ctx, classID, id, false)
	if err != nil {
		return AgentConfig{}, err
	}
	out := AgentConfig{Agent: a, SkillIDs: []int64{}, MaterialIDs: []int64{}}
	for _, item := range []struct {
		query string
		dest  *[]int64
	}{{`SELECT skill_id FROM course_agent_skills WHERE agent_id=? AND class_id=? ORDER BY skill_id`, &out.SkillIDs}, {`SELECT material_id FROM course_agent_materials WHERE agent_id=? AND class_id=? ORDER BY material_id`, &out.MaterialIDs}} {
		rows, e := s.db.QueryContext(ctx, item.query, id, classID)
		if e != nil {
			return AgentConfig{}, ErrLesson5Unavailable
		}
		for rows.Next() {
			var n int64
			if rows.Scan(&n) != nil {
				rows.Close()
				return AgentConfig{}, ErrLesson5Unavailable
			}
			*item.dest = append(*item.dest, n)
		}
		if rows.Err() != nil {
			rows.Close()
			return AgentConfig{}, ErrLesson5Unavailable
		}
		rows.Close()
	}
	return out, nil
}

func (s *Lesson5Store) SaveSkill(ctx context.Context, u repositories.User, id int64, raw string) (int64, error) {
	if u.Role != "teacher" {
		return 0, ErrNotTeacher
	}
	doc, err := ParseSkillDocument(raw)
	if err != nil {
		return 0, ErrLesson5Invalid
	}
	tools, _ := json.Marshal(doc.Tools)
	var result sql.Result
	if id > 0 {
		var exists int
		if s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM course_skills WHERE id=? AND class_id=?`, id, u.ClassID).Scan(&exists) != nil || exists != 1 {
			return 0, ErrLesson5NotFound
		}
		result, err = s.db.ExecContext(ctx, `UPDATE course_skills SET name=?,trigger_text=?,subject_text=?,tools_json=?,enabled=?,description=?,source_text=? WHERE id=? AND class_id=?`, doc.Name, doc.TriggerText, doc.SubjectText, string(tools), doc.Enabled, doc.Description, doc.SourceText, id, u.ClassID)
	} else {
		result, err = s.db.ExecContext(ctx, `INSERT INTO course_skills(class_id,name,trigger_text,subject_text,tools_json,enabled,description,source_text) VALUES(?,?,?,?,?,?,?,?)`, u.ClassID, doc.Name, doc.TriggerText, doc.SubjectText, string(tools), doc.Enabled, doc.Description, doc.SourceText)
	}
	if err != nil {
		return 0, ErrLesson5Invalid
	}
	if id > 0 {
		// The row's class-scoped existence was checked before UPDATE. MySQL can
		// report zero changed rows when the teacher saves identical text; that
		// is still a successful edit, not a missing skill.
		_ = result
		return id, nil
	}
	id, err = result.LastInsertId()
	if err != nil {
		return 0, ErrLesson5Unavailable
	}
	return id, nil
}
func (s *Lesson5Store) ListSkills(ctx context.Context, classID int64) ([]StoredSkill, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,trigger_text,subject_text,enabled,description FROM course_skills WHERE class_id=? ORDER BY id`, classID)
	if err != nil {
		return nil, ErrLesson5Unavailable
	}
	defer rows.Close()
	out := []StoredSkill{}
	for rows.Next() {
		var x StoredSkill
		if rows.Scan(&x.ID, &x.Name, &x.TriggerText, &x.SubjectText, &x.Enabled, &x.Description) != nil {
			return nil, ErrLesson5Unavailable
		}
		out = append(out, x)
	}
	if rows.Err() != nil {
		return nil, ErrLesson5Unavailable
	}
	return out, nil
}

func (s *Lesson5Store) SkillSource(ctx context.Context, classID, id int64) (string, error) {
	var raw string
	if s.db.QueryRowContext(ctx, `SELECT source_text FROM course_skills WHERE id=? AND class_id=?`, id, classID).Scan(&raw) != nil {
		return "", ErrLesson5NotFound
	}
	return raw, nil
}

func (s *Lesson5Store) DeleteSkill(ctx context.Context, u repositories.User, id int64) error {
	if u.Role != "teacher" {
		return ErrNotTeacher
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM course_skills WHERE id=? AND class_id=?`, id, u.ClassID)
	if err != nil {
		return ErrLesson5Unavailable
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrLesson5NotFound
	}
	return nil
}

func (s *Lesson5Store) ListConversations(ctx context.Context, u repositories.User) ([]Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,agent_id,title,DATE_FORMAT(updated_at,'%Y-%m-%dT%H:%i:%sZ') FROM conversations WHERE user_id=? AND class_id=? ORDER BY updated_at DESC,id DESC`, u.ID, u.ClassID)
	if err != nil {
		return nil, ErrLesson5Unavailable
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		if rows.Scan(&c.ID, &c.AgentID, &c.Title, &c.UpdatedAt) != nil {
			return nil, ErrLesson5Unavailable
		}
		out = append(out, c)
	}
	if rows.Err() != nil {
		return nil, ErrLesson5Unavailable
	}
	return out, nil
}

func (s *Lesson5Store) CreateConversation(ctx context.Context, u repositories.User, agentID int64) (int64, error) {
	var count int
	if s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM course_agents WHERE class_id=? AND enabled=1`, u.ClassID).Scan(&count) != nil {
		return 0, ErrLesson5Unavailable
	}
	if count == 0 {
		return 0, ErrNoEnabledAgent
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO conversations(class_id,user_id,agent_id,title) SELECT ?,?,?, '新的提问' FROM course_agents WHERE id=? AND class_id=? AND enabled=1`, u.ClassID, u.ID, agentID, agentID, u.ClassID)
	if err != nil {
		return 0, ErrLesson5Unavailable
	}
	if affected, e := res.RowsAffected(); e != nil || affected != 1 {
		return 0, ErrLesson5NotFound
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, ErrLesson5Unavailable
	}
	if id == 0 {
		return 0, ErrLesson5NotFound
	}
	return id, nil
}

var ErrNoEnabledAgent = errors.New("本班暂无可用智能体")

func (s *Lesson5Store) ReadConversation(ctx context.Context, u repositories.User, id int64) (Conversation, []StoredMessage, int64, error) {
	var c Conversation
	if s.db.QueryRowContext(ctx, `SELECT id,agent_id,title,DATE_FORMAT(updated_at,'%Y-%m-%dT%H:%i:%sZ') FROM conversations WHERE id=? AND user_id=? AND class_id=?`, id, u.ID, u.ClassID).Scan(&c.ID, &c.AgentID, &c.Title, &c.UpdatedAt) != nil {
		return c, nil, 0, ErrLesson5NotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,role,content,citations_json,skill_name FROM conversation_messages WHERE conversation_id=? AND user_id=? AND class_id=? ORDER BY id`, id, u.ID, u.ClassID)
	if err != nil {
		return c, nil, 0, ErrLesson5Unavailable
	}
	defer rows.Close()
	msgs := []StoredMessage{}
	var last int64
	for rows.Next() {
		var m StoredMessage
		var cites sql.NullString
		var skill sql.NullString
		if rows.Scan(&m.ID, &m.Role, &m.Content, &cites, &skill) != nil {
			return c, nil, 0, ErrLesson5Unavailable
		}
		if cites.Valid {
			_ = json.Unmarshal([]byte(cites.String), &m.Citations)
		}
		if m.Citations == nil {
			m.Citations = []KnowledgeHit{}
		}
		if skill.Valid {
			m.SkillName = &skill.String
		}
		if m.Role == "user" {
			last = m.ID
		}
		msgs = append(msgs, m)
	}
	return c, msgs, last, nil
}

// AgentContext returns the latest mutable configuration for an already locked conversation.
func (s *Lesson5Store) AgentContext(ctx context.Context, u repositories.User, conversationID int64) (Agent, []RoutedSkill, []int64, error) {
	var a Agent
	err := s.db.QueryRowContext(ctx, `SELECT a.id,a.name,a.prompt,a.enabled,COALESCE(b.builtin_key,'') FROM conversations c JOIN course_agents a ON a.id=c.agent_id AND a.class_id=c.class_id LEFT JOIN course_builtin_agents b ON b.agent_id=a.id AND b.class_id=a.class_id WHERE c.id=? AND c.user_id=? AND c.class_id=?`, conversationID, u.ID, u.ClassID).Scan(&a.ID, &a.Name, &a.Prompt, &a.Enabled, &a.BuiltinKey)
	if err != nil {
		return Agent{}, nil, nil, ErrLesson5NotFound
	}
	if a.Prompt == "" {
		a.Prompt = "你是软件开发课程的学习助手。用简洁中文帮助学生理解概念、分析问题并给出下一步建议。"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,s.name,s.trigger_text,s.description,s.subject_text,s.enabled FROM course_agent_skills b JOIN course_skills s ON s.id=b.skill_id AND s.class_id=b.class_id WHERE b.agent_id=? AND b.class_id=? ORDER BY s.id`, a.ID, u.ClassID)
	if err != nil {
		return Agent{}, nil, nil, ErrLesson5Unavailable
	}
	skills := []RoutedSkill{}
	for rows.Next() {
		var x RoutedSkill
		x.Bound = true
		if rows.Scan(&x.ID, &x.Name, &x.TriggerText, &x.Description, &x.SubjectText, &x.Enabled) != nil {
			rows.Close()
			return Agent{}, nil, nil, ErrLesson5Unavailable
		}
		skills = append(skills, x)
	}
	if rows.Err() != nil {
		rows.Close()
		return Agent{}, nil, nil, ErrLesson5Unavailable
	}
	rows.Close()
	rows, err = s.db.QueryContext(ctx, `SELECT material_id FROM course_agent_materials WHERE agent_id=? AND class_id=? ORDER BY material_id`, a.ID, u.ClassID)
	if err != nil {
		return Agent{}, nil, nil, ErrLesson5Unavailable
	}
	mats := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) != nil {
			rows.Close()
			return Agent{}, nil, nil, ErrLesson5Unavailable
		}
		mats = append(mats, id)
	}
	if rows.Err() != nil {
		rows.Close()
		return Agent{}, nil, nil, ErrLesson5Unavailable
	}
	rows.Close()
	return a, skills, mats, nil
}

func (s *Lesson5Store) AppendQuestion(ctx context.Context, u repositories.User, cid int64, question string) (int64, []ChatMessage, error) {
	if n := utf8.RuneCountInString(strings.TrimSpace(question)); n < 2 || n > 500 {
		return 0, nil, ErrLesson5Invalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, ErrLesson5Unavailable
	}
	defer tx.Rollback()
	var title string
	if tx.QueryRowContext(ctx, `SELECT title FROM conversations WHERE id=? AND user_id=? AND class_id=? FOR UPDATE`, cid, u.ID, u.ClassID).Scan(&title) != nil {
		return 0, nil, ErrLesson5NotFound
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO conversation_messages(conversation_id,user_id,class_id,role,content) VALUES(?,?,?,'user',?)`, cid, u.ID, u.ClassID, strings.TrimSpace(question)); err != nil {
		return 0, nil, ErrLesson5Unavailable
	}
	if title == "新的提问" {
		r := []rune(strings.TrimSpace(question))
		if len(r) > 24 {
			r = r[:24]
		}
		title = string(r)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE conversations SET title=?,updated_at=CURRENT_TIMESTAMP(6) WHERE id=? AND user_id=? AND class_id=?`, title, cid, u.ID, u.ClassID); err != nil {
		return 0, nil, ErrLesson5Unavailable
	}
	rows, err := tx.QueryContext(ctx, `SELECT role,LEFT(content,2000) FROM conversation_messages WHERE conversation_id=? AND user_id=? AND class_id=? AND role IN ('user','assistant') AND content<>'' ORDER BY id DESC LIMIT 6`, cid, u.ID, u.ClassID)
	if err != nil {
		return 0, nil, ErrLesson5Unavailable
	}
	reverse := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		if rows.Scan(&m.Role, &m.Content) != nil {
			rows.Close()
			return 0, nil, ErrLesson5Unavailable
		}
		r := []rune(m.Content)
		if len(r) > 500 {
			m.Content = string(r[:500])
		}
		reverse = append(reverse, m)
	}
	if rows.Err() != nil {
		rows.Close()
		return 0, nil, ErrLesson5Unavailable
	}
	rows.Close()
	if err = tx.Commit(); err != nil {
		return 0, nil, ErrLesson5Unavailable
	}
	out := make([]ChatMessage, 0, len(reverse))
	for i := len(reverse) - 1; i >= 0; i-- {
		out = append(out, reverse[i])
	}
	var userID int64
	if s.db.QueryRowContext(ctx, `SELECT id FROM conversation_messages WHERE conversation_id=? AND user_id=? AND class_id=? AND role='user' ORDER BY id DESC LIMIT 1`, cid, u.ID, u.ClassID).Scan(&userID) != nil {
		return 0, nil, ErrLesson5Unavailable
	}
	return userID, out, nil
}

func (s *Lesson5Store) SaveAnswer(ctx context.Context, u repositories.User, cid int64, content string, citations []KnowledgeHit, skillName string) (int64, error) {
	data, err := json.Marshal(citations)
	if err != nil {
		return 0, ErrLesson5Unavailable
	}
	var skill any
	if skillName != "" {
		skill = skillName
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO conversation_messages(conversation_id,user_id,class_id,role,content,citations_json,skill_name) SELECT c.id,c.user_id,c.class_id,'assistant',?,?,? FROM conversations c WHERE c.id=? AND c.user_id=? AND c.class_id=?`, content, string(data), skill, cid, u.ID, u.ClassID)
	if err != nil {
		return 0, ErrLesson5Unavailable
	}
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		return 0, ErrLesson5NotFound
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE conversations SET updated_at=CURRENT_TIMESTAMP(6) WHERE id=? AND user_id=? AND class_id=?`, cid, u.ID, u.ClassID); err != nil {
		return 0, ErrLesson5Unavailable
	}
	return id, nil
}
