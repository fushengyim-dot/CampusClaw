package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"campusclaw/backend/repositories"
)

const MaxMaterialBytes = 5 * 1024 * 1024

var (
	ErrMaterialsUnavailable = errors.New("材料服务暂不可用")
	ErrUploadUnknown        = errors.New("上传结果暂未确认，请稍后刷新列表确认，勿重复上传")
	ErrUploadRejected       = errors.New("上传内容不符合要求")
	ErrUnsupportedType      = errors.New("不支持的讲义文件类型")
	ErrUploadFailed         = errors.New("已确认讲义未保存")
	ErrNotTeacher           = errors.New("只有教师可以上传讲义")
	ErrMaterialNotFound     = errors.New("请求的资源不存在")
	ErrPage                 = errors.New("分页参数不正确")
)

type Material struct {
	ID               int64     `json:"id"`
	Title            string    `json:"title"`
	OriginalFilename string    `json:"original_filename"`
	SizeBytes        int64     `json:"size_bytes"`
	SourceURL        *string   `json:"source_url"`
	SourceLocator    *string   `json:"source_locator"`
	CreatedAt        time.Time `json:"created_at"`
}
type MaterialDetail struct {
	Material
	Content string `json:"content"`
}
type MaterialPage struct {
	Items    []Material `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}
type Download struct {
	Filename string
	Path     string
	Size     int64
}

type Materials struct {
	db              *sql.DB
	root            string
	ready           atomic.Bool
	recoveryAllowed atomic.Bool
	gate            sync.RWMutex
	testHooks       *materialTestHooks
}

type materialTestHooks struct {
	fileWrite             func(*os.File, []byte) error
	beforeMaterialInsert  func() error
	beforeKnowledgeInsert func() error
	beforeCommit          func() error
	commit                func(*sql.Tx) error
	afterCommit           func() error
	removeFile            func(string) error
}

var storageNamePattern = regexp.MustCompile(`^[a-f0-9]{32}\.(txt|md)$`)

func NewMaterials(db *sql.DB, root string) (*Materials, error) {
	if db == nil {
		return nil, ErrMaterialsUnavailable
	}
	if strings.TrimSpace(root) == "" {
		root = filepath.Join("..", "runtime", "uploads")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, ErrMaterialsUnavailable
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, ErrMaterialsUnavailable
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrMaterialsUnavailable
	}
	s := &Materials{db: db, root: root}
	s.recoveryAllowed.Store(true)
	return s, nil
}

func (s *Materials) Ready() bool { return s.ready.Load() }

func (s *Materials) setReady(v bool) { s.ready.Store(v) }

func (s *Materials) SetUnavailable() { s.recoveryAllowed.Store(false); s.setReady(false) }

func (s *Materials) removeFile(path string) error {
	if s.testHooks != nil && s.testHooks.removeFile != nil {
		return s.testHooks.removeFile(path)
	}
	return os.Remove(path)
}

func (s *Materials) withUploadLock(ctx context.Context, fn func(*sql.Conn) error) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return ErrMaterialsUnavailable
	}
	defer conn.Close()
	var dbName string
	if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&dbName); err != nil || dbName == "" {
		return ErrMaterialsUnavailable
	}
	lock := "campusclaw-upload-" + fmt.Sprintf("%x", sha256.Sum256([]byte(dbName)))[:32]
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 10)", lock).Scan(&got); err != nil || !got.Valid || got.Int64 != 1 {
		return ErrMaterialsUnavailable
	}
	defer func() {
		releaseCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		var released sql.NullInt64
		if conn.QueryRowContext(releaseCtx, "SELECT RELEASE_LOCK(?)", lock).Scan(&released) != nil || !released.Valid || released.Int64 != 1 {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	return fn(conn)
}

// driverBadConn forces database/sql to discard the physical session and release a
// possibly-held MySQL named lock instead of returning it to the connection pool.
func (s *Materials) Recover(ctx context.Context) error {
	s.setReady(false)
	if !s.recoveryAllowed.Load() {
		return ErrMaterialsUnavailable
	}
	s.gate.Lock()
	defer s.gate.Unlock()
	err := s.withUploadLock(ctx, func(conn *sql.Conn) error { return s.reconcile(ctx, conn) })
	if err != nil {
		return ErrMaterialsUnavailable
	}
	s.setReady(true)
	return nil
}

type storedMaterial struct {
	classID      int64
	key          string
	size         int64
	hash         string
	content      sql.NullString
	contentClass sql.NullInt64
}

func (s *Materials) reconcile(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, `SELECT m.class_id,m.storage_key,m.size_bytes,m.sha256,k.content,k.class_id
		FROM materials m LEFT JOIN knowledge_entries k ON k.material_id=m.id ORDER BY m.id`)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for rows.Next() {
		var x storedMaterial
		if err := rows.Scan(&x.classID, &x.key, &x.size, &x.hash, &x.content, &x.contentClass); err != nil {
			rows.Close()
			return err
		}
		if !storageNamePattern.MatchString(x.key) || x.classID <= 0 || !x.content.Valid || !x.contentClass.Valid || x.contentClass.Int64 != x.classID {
			rows.Close()
			return errors.New("incomplete material record")
		}
		path := filepath.Join(s.root, strconv.FormatInt(x.classID, 10), x.key)
		data, err := readRegularNoLink(path, MaxMaterialBytes)
		if err != nil || int64(len(data)) != x.size || fmt.Sprintf("%x", sha256.Sum256(data)) != x.hash {
			rows.Close()
			return errors.New("material file mismatch")
		}
		if !strings.EqualFold(strings.TrimPrefix(string(data), "\uFEFF"), strings.TrimPrefix(x.content.String, "\uFEFF")) {
			rows.Close()
			return errors.New("material body mismatch")
		}
		known[filepath.Join(strconv.FormatInt(x.classID, 10), x.key)] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	classes, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	for _, class := range classes {
		if !class.IsDir() || class.Type()&os.ModeSymlink != 0 {
			return errors.New("unexpected upload directory")
		}
		if _, err := strconv.ParseInt(class.Name(), 10, 64); err != nil {
			return errors.New("unexpected upload directory")
		}
		folder := filepath.Join(s.root, class.Name())
		files, err := os.ReadDir(folder)
		if err != nil {
			return err
		}
		for _, f := range files {
			rel := filepath.Join(class.Name(), f.Name())
			if !storageNamePattern.MatchString(f.Name()) || !f.Type().IsRegular() || f.Type()&os.ModeSymlink != 0 {
				return errors.New("unexpected upload file")
			}
			if !known[rel] {
				if err := s.removeFile(filepath.Join(folder, f.Name())); err != nil {
					return err
				}
			}
		}
		if len(files) == 0 {
			_ = os.Remove(folder)
		}
	}
	return nil
}

func readRegularNoLink(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("not regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit+1))
}

func (s *Materials) List(ctx context.Context, classID int64, page, size int) (MaterialPage, error) {
	s.gate.RLock()
	defer s.gate.RUnlock()
	if !s.Ready() {
		return MaterialPage{}, ErrMaterialsUnavailable
	}
	if page < 1 || size < 1 || size > 100 {
		return MaterialPage{}, ErrPage
	}
	if int64(page-1) > math.MaxInt64/int64(size) {
		return MaterialPage{}, ErrPage
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM materials WHERE class_id=?", classID).Scan(&total); err != nil {
		return MaterialPage{}, ErrMaterialsUnavailable
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,original_filename,size_bytes,source_url,source_locator,created_at FROM materials WHERE class_id=? ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?`, classID, size, int64(page-1)*int64(size))
	if err != nil {
		return MaterialPage{}, ErrMaterialsUnavailable
	}
	defer rows.Close()
	items := []Material{}
	for rows.Next() {
		var m Material
		var u, l sql.NullString
		if rows.Scan(&m.ID, &m.Title, &m.OriginalFilename, &m.SizeBytes, &u, &l, &m.CreatedAt) != nil {
			return MaterialPage{}, ErrMaterialsUnavailable
		}
		if u.Valid {
			m.SourceURL = &u.String
		}
		if l.Valid {
			m.SourceLocator = &l.String
		}
		items = append(items, m)
	}
	if rows.Err() != nil {
		return MaterialPage{}, ErrMaterialsUnavailable
	}
	return MaterialPage{Items: items, Total: total, Page: page, PageSize: size}, nil
}

func (s *Materials) Detail(ctx context.Context, classID, id int64) (MaterialDetail, error) {
	s.gate.RLock()
	defer s.gate.RUnlock()
	if !s.Ready() {
		return MaterialDetail{}, ErrMaterialsUnavailable
	}
	var d MaterialDetail
	var u, l sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT m.id,m.title,m.original_filename,m.size_bytes,m.source_url,m.source_locator,m.created_at,k.content FROM materials m JOIN knowledge_entries k ON k.material_id=m.id AND k.class_id=m.class_id WHERE m.id=? AND m.class_id=?`, id, classID).Scan(&d.ID, &d.Title, &d.OriginalFilename, &d.SizeBytes, &u, &l, &d.CreatedAt, &d.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return MaterialDetail{}, ErrMaterialNotFound
	}
	if err != nil {
		return MaterialDetail{}, ErrMaterialsUnavailable
	}
	if u.Valid {
		d.SourceURL = &u.String
	}
	if l.Valid {
		d.SourceLocator = &l.String
	}
	return d, nil
}

func (s *Materials) OpenDownload(ctx context.Context, classID, id int64) (Download, error) {
	s.gate.RLock()
	defer s.gate.RUnlock()
	if !s.Ready() {
		return Download{}, ErrMaterialsUnavailable
	}
	var d Download
	var key, expectedHash string
	err := s.db.QueryRowContext(ctx, "SELECT original_filename,storage_key,size_bytes,sha256 FROM materials WHERE id=? AND class_id=?", id, classID).Scan(&d.Filename, &key, &d.Size, &expectedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Download{}, ErrMaterialNotFound
	}
	if err != nil {
		return Download{}, ErrMaterialsUnavailable
	}
	if !storageNamePattern.MatchString(key) {
		return Download{}, ErrMaterialsUnavailable
	}
	var contentClass int64
	var content string
	if err := s.db.QueryRowContext(ctx, "SELECT class_id,content FROM knowledge_entries WHERE material_id=?", id).Scan(&contentClass, &content); err != nil || contentClass != classID {
		return Download{}, ErrMaterialsUnavailable
	}
	d.Path = filepath.Join(s.root, strconv.FormatInt(classID, 10), key)
	data, err := readRegularNoLink(d.Path, MaxMaterialBytes)
	if err != nil || int64(len(data)) != d.Size || fmt.Sprintf("%x", sha256.Sum256(data)) != expectedHash {
		return Download{}, ErrMaterialsUnavailable
	}
	return d, nil
}

func (s *Materials) Upload(ctx context.Context, user repositories.User, title, filename, sourceURL, locator string, data []byte) (int64, error) {
	s.gate.RLock()
	if !s.Ready() {
		s.gate.RUnlock()
		return 0, ErrMaterialsUnavailable
	}
	id, err := s.uploadLocked(ctx, user, title, filename, sourceURL, locator, data)
	s.gate.RUnlock()
	if !errors.Is(err, ErrUploadUnknown) {
		return id, err
	}
	if recErr := s.Recover(ctx); recErr != nil {
		return 0, ErrUploadUnknown
	}
	var found int
	e := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM materials m JOIN knowledge_entries k ON k.material_id=m.id AND k.class_id=m.class_id WHERE m.id=? AND m.class_id=?", id, user.ClassID).Scan(&found)
	if e == nil && found == 1 {
		return id, nil
	}
	if e != nil {
		s.setReady(false)
		return 0, ErrUploadUnknown
	}
	return 0, ErrUploadFailed
}

func (s *Materials) uploadLocked(ctx context.Context, user repositories.User, title, filename, sourceURL, locator string, data []byte) (int64, error) {
	if !s.Ready() {
		return 0, ErrMaterialsUnavailable
	}
	if user.Role != "teacher" {
		return 0, ErrNotTeacher
	}
	if user.ClassID <= 0 {
		return 0, ErrUploadRejected
	}
	if len(data) > MaxMaterialBytes {
		return 0, ErrUploadRejected
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".txt" && ext != ".md" {
		return 0, ErrUnsupportedType
	}
	if len(data) == 0 || !validMaterialContent(data) {
		return 0, ErrUploadRejected
	}
	title = strings.TrimSpace(title)
	if title == "" || !utf8.ValidString(title) || strings.ContainsRune(title, '\x00') || len([]rune(title)) > 200 || !utf8.ValidString(filename) || len([]rune(filename)) > 255 || !utf8.ValidString(locator) || len([]rune(locator)) > 200 || !utf8.ValidString(sourceURL) || len([]rune(sourceURL)) > 2048 {
		return 0, ErrUploadRejected
	}
	if sourceURL != "" {
		u, e := url.Parse(sourceURL)
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return 0, ErrUploadRejected
		}
	}
	content := strings.TrimPrefix(string(data), "\uFEFF")
	var id int64
	err := s.withUploadLock(ctx, func(conn *sql.Conn) error {
		var random [16]byte
		if _, e := rand.Read(random[:]); e != nil {
			return ErrMaterialsUnavailable
		}
		key := hex.EncodeToString(random[:]) + ext
		dir := filepath.Join(s.root, strconv.FormatInt(user.ClassID, 10))
		if e := os.MkdirAll(dir, 0700); e != nil {
			return ErrMaterialsUnavailable
		}
		dirInfo, e := os.Lstat(dir)
		if e != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
			return ErrMaterialsUnavailable
		}
		path := filepath.Join(dir, key)
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return ErrUploadFailed
		}
		if s.testHooks != nil && s.testHooks.fileWrite != nil {
			e = s.testHooks.fileWrite(f, data)
		} else {
			_, e = f.Write(data)
		}
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			removeErr := s.removeFile(path)
			if removeErr != nil && !os.IsNotExist(removeErr) {
				s.setReady(false)
				return ErrUploadUnknown
			}
			return ErrUploadFailed
		}
		deleteOwned := func() error {
			e := s.removeFile(path)
			if os.IsNotExist(e) {
				return nil
			}
			if e == nil {
				_ = os.Remove(dir)
			}
			return e
		}
		tx, e := conn.BeginTx(ctx, nil)
		if e != nil {
			if deleteOwned() != nil {
				s.setReady(false)
				return ErrUploadUnknown
			}
			return ErrMaterialsUnavailable
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(data))
		var source any
		if sourceURL != "" {
			source = sourceURL
		}
		var place any
		if locator != "" {
			place = locator
		}
		if s.testHooks != nil && s.testHooks.beforeMaterialInsert != nil {
			e = s.testHooks.beforeMaterialInsert()
		}
		var res sql.Result
		if e == nil {
			res, e = tx.ExecContext(ctx, "INSERT INTO materials(class_id,uploaded_by,title,original_filename,storage_key,size_bytes,sha256,source_url,source_locator) VALUES(?,?,?,?,?,?,?,?,?)", user.ClassID, user.ID, title, filename, key, len(data), digest, source, place)
		}
		if e == nil {
			id, e = res.LastInsertId()
		}
		if e == nil {
			if s.testHooks != nil && s.testHooks.beforeKnowledgeInsert != nil {
				e = s.testHooks.beforeKnowledgeInsert()
			}
			if e == nil {
				var entry sql.Result
				entry, e = tx.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,content) VALUES(?,?,?)", id, user.ClassID, content)
				if e == nil {
					var entryID int64
					entryID, e = entry.LastInsertId()
					if e == nil {
						var chunks []TextChunk
						chunks, e = ChunkText(content, ChunkOptions{})
						e = insertChunks(ctx, tx, entryID, chunks)
					}
				}
			}
		}
		if e != nil {
			rb := tx.Rollback()
			if rb != nil {
				s.setReady(false)
				return ErrUploadUnknown
			}
			if deleteOwned() != nil {
				s.setReady(false)
				return ErrUploadUnknown
			}
			return ErrUploadFailed
		}
		if s.testHooks != nil && s.testHooks.beforeCommit != nil {
			if e = s.testHooks.beforeCommit(); e != nil {
				if tx.Rollback() != nil {
					s.setReady(false)
					return ErrUploadUnknown
				}
				if deleteOwned() != nil {
					s.setReady(false)
					return ErrUploadUnknown
				}
				return ErrUploadFailed
			}
		}
		if s.testHooks != nil && s.testHooks.commit != nil {
			e = s.testHooks.commit(tx)
		} else {
			e = tx.Commit()
		}
		if e == nil {
			if s.testHooks != nil && s.testHooks.afterCommit != nil {
				e = s.testHooks.afterCommit()
			}
			if e == nil {
				return nil
			}
		}
		if e == nil {
			return nil
		}
		// COMMIT may have reached MySQL even if its reply was lost. Drop this
		// session, then acquire the same DB lock on a fresh session before deciding.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		s.setReady(false)
		return ErrUploadUnknown
	})
	if err == nil {
		return id, nil
	}
	if errors.Is(err, ErrUploadUnknown) {
		return id, ErrUploadUnknown
	}
	return 0, err
}

func insertChunks(ctx context.Context, tx *sql.Tx, entryID int64, chunks []TextChunk) error {
	const batchSize = 100
	for start := 0; start < len(chunks); start += batchSize {
		end := start + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		var query strings.Builder
		query.WriteString("INSERT INTO knowledge_chunks(knowledge_entry_id,chunk_index,chunk_text,char_start,char_end,index_status) VALUES ")
		args := make([]any, 0, (end-start)*5)
		for i := start; i < end; i++ {
			if i > start {
				query.WriteByte(',')
			}
			query.WriteString("(?,?,?,?,?,'ready')")
			chunk := chunks[i]
			args = append(args, entryID, chunk.Index, chunk.Text, chunk.Start, chunk.End)
		}
		if _, err := tx.ExecContext(ctx, query.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func validMaterialContent(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	text := strings.TrimPrefix(string(data), "\uFEFF")
	if strings.ContainsRune(text, '\x00') || strings.TrimSpace(text) == "" {
		return false
	}
	return true
}
