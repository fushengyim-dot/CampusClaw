package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"campusclaw/backend/handlers"
	"campusclaw/backend/repositories"
	"campusclaw/backend/services"
	"golang.org/x/crypto/bcrypt"
)

type registrationMaterialsStub struct{}

func (registrationMaterialsStub) Ready() bool                   { return true }
func (registrationMaterialsStub) Recover(context.Context) error { return nil }
func (registrationMaterialsStub) List(context.Context, int64, int, int) (services.MaterialPage, error) {
	return services.MaterialPage{}, nil
}
func (registrationMaterialsStub) Detail(context.Context, int64, int64) (services.MaterialDetail, error) {
	return services.MaterialDetail{}, services.ErrMaterialNotFound
}
func (registrationMaterialsStub) OpenDownload(context.Context, int64, int64) (services.Download, error) {
	return services.Download{}, services.ErrMaterialNotFound
}
func (registrationMaterialsStub) Upload(context.Context, repositories.User, string, string, string, string, []byte) (int64, error) {
	return 0, services.ErrNotTeacher
}

func TestAccountRegistrationIntegration(t *testing.T) {
	db, ctx, _ := integrationDB(t)
	if err := InitSchema(ctx, db); err != nil {
		t.Fatal("registration schema unavailable")
	}
	suffix := make([]byte, 10)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal("fixture id unavailable")
	}
	tag := "registration-check-" + hex.EncodeToString(suffix)
	classResult, err := db.ExecContext(ctx, `INSERT INTO classes(name) VALUES(?)`, tag)
	if err != nil {
		t.Fatal("synthetic class setup failed")
	}
	classID, _ := classResult.LastInsertId()
	teacherResult, err := db.ExecContext(ctx, `INSERT INTO users(username,password_hash,role,class_id) VALUES(?,?,?,?)`, tag+"-teacher", "!synthetic-disabled", "teacher", classID)
	if err != nil {
		t.Fatal("synthetic teacher setup failed")
	}
	teacherID, _ := teacherResult.LastInsertId()
	teacher := repositories.User{ID: teacherID, Username: tag + "-teacher", Role: "teacher", ClassID: classID}
	teacherHash, err := bcrypt.GenerateFromPassword([]byte("registration-integration-teacher-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal("synthetic teacher credential setup failed")
	}
	if _, err = db.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, string(teacherHash), teacherID); err != nil {
		t.Fatal("synthetic teacher credential setup failed")
	}
	t.Cleanup(func() {
		clean, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = db.ExecContext(clean, `DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE class_id=?)`, classID)
		_, _ = db.ExecContext(clean, `DELETE FROM account_invitations WHERE class_id=?`, classID)
		_, _ = db.ExecContext(clean, `DELETE FROM users WHERE class_id=?`, classID)
		_, _ = db.ExecContext(clean, `DELETE FROM classes WHERE id=? AND name=?`, classID, tag)
	})
	store := services.NewRegistrationStore(db)
	if _, _, err = store.Issue(ctx, repositories.User{Role: "student", ClassID: classID}, "student"); err == nil {
		t.Fatal("student issued an invitation")
	}
	if _, _, err = store.Issue(ctx, teacher, "admin"); err == nil {
		t.Fatal("unsupported role invitation accepted")
	}
	item, code, err := store.Issue(ctx, teacher, "student")
	if err != nil {
		t.Fatal("teacher invitation issue failed")
	}
	if store.Register(ctx, code, tag+"-short-password", "short") == nil {
		t.Fatal("short password was accepted")
	}
	var stored string
	if db.QueryRowContext(ctx, `SELECT token_hash FROM account_invitations WHERE id=?`, item.ID).Scan(&stored) != nil || stored == code {
		t.Fatal("invitation plaintext was stored")
	}
	if err = store.Register(ctx, code, tag+"-student", "registration-test-passphrase"); err != nil {
		t.Fatal("valid student registration failed")
	}
	var role string
	var actualClass int64
	if db.QueryRowContext(ctx, `SELECT role,class_id FROM users WHERE username=?`, tag+"-student").Scan(&role, &actualClass) != nil || role != "student" || actualClass != classID {
		t.Fatal("account role/class did not match invitation")
	}
	if store.Register(ctx, code, tag+"-replay", "registration-test-passphrase") == nil {
		t.Fatal("consumed invitation was reused")
	}
	if store.Register(ctx, strings.Repeat("f", 64), tag+"-invalid-code", "registration-test-passphrase") == nil {
		t.Fatal("invalid invitation was accepted")
	}
	duplicateItem, duplicateCode, err := store.Issue(ctx, teacher, "student")
	if err != nil || store.Register(ctx, duplicateCode, tag+"-teacher", "registration-test-passphrase") == nil || store.Register(ctx, duplicateCode, tag+"-after-duplicate", "registration-test-passphrase") != nil {
		t.Fatal("duplicate username did not fail generically while preserving the invitation")
	}
	_ = duplicateItem
	teacherCodeItem, teacherCode, err := store.Issue(ctx, teacher, "teacher")
	if err != nil || store.Register(ctx, teacherCode, tag+"-teacher2", "registration-test-passphrase") != nil {
		t.Fatal("valid teacher registration failed")
	}
	if db.QueryRowContext(ctx, `SELECT role,class_id FROM users WHERE username=?`, tag+"-teacher2").Scan(&role, &actualClass) != nil || role != "teacher" || actualClass != classID {
		t.Fatal("teacher account role/class did not match invitation")
	}
	if err = store.Revoke(ctx, teacher, teacherCodeItem.ID); err == nil {
		t.Fatal("consumed teacher invitation was revocable")
	}
	revoked, revokedCode, err := store.Issue(ctx, teacher, "student")
	if err != nil || store.Revoke(ctx, teacher, revoked.ID) != nil || store.Register(ctx, revokedCode, tag+"-revoked", "registration-test-passphrase") == nil {
		t.Fatal("revoked invitation remained usable")
	}
	expired, expiredCode, err := store.Issue(ctx, teacher, "student")
	if err != nil {
		t.Fatal("expired invitation setup failed")
	}
	if _, err = db.ExecContext(ctx, `UPDATE account_invitations SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE id=?`, expired.ID); err != nil || store.Register(ctx, expiredCode, tag+"-expired", "registration-test-passphrase") == nil {
		t.Fatal("expired invitation remained usable")
	}

	concurrent, concurrentCode, err := store.Issue(ctx, teacher, "teacher")
	if err != nil {
		t.Fatal("concurrent invitation setup failed")
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for _, name := range []string{tag + "-race-a", tag + "-race-b"} {
		wg.Add(1)
		go func(username string) {
			defer wg.Done()
			outcomes <- store.Register(ctx, concurrentCode, username, "registration-test-passphrase")
		}(name)
	}
	wg.Wait()
	close(outcomes)
	successes := 0
	for e := range outcomes {
		if e == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("single-use concurrent invitation had %d successful consumers", successes)
	}
	var created int
	if db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username IN (?,?)`, tag+"-race-a", tag+"-race-b").Scan(&created) != nil || created != 1 {
		t.Fatal("concurrent invitation left an unexpected number of accounts")
	}
	list, err := store.List(ctx, teacher)
	if err != nil || len(list) < 5 {
		t.Fatal("teacher invitation list unavailable")
	}
	if err = store.Revoke(ctx, teacher, concurrent.ID); err == nil {
		t.Fatal("consumed invitation was revocable as unused")
	}
	// Exercise the actual HTTP routes and the existing HttpOnly session flow.
	auth, err := services.NewAuth(repositories.NewSessions(db), "registration-integration-session-secret", nil)
	if err != nil {
		t.Fatal("registration test session service unavailable")
	}
	app := handlers.NewApplicationWithLesson6(auth, registrationMaterialsStub{}, "http://registration.test", nil, nil, nil, nil, store)
	_, teacherSession, _, err := auth.Login(ctx, teacher.Username, "registration-integration-teacher-password", "")
	if err != nil {
		t.Fatal("synthetic teacher session setup failed")
	}
	httpIssue := func(cookie, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/teacher/invitations", strings.NewReader(body))
		r.Header.Set("Origin", "http://registration.test")
		r.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: cookie})
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w
	}
	if w := httpIssue("", `{"role":"student"}`); w.Code != http.StatusUnauthorized {
		t.Fatal("unauthenticated invitation endpoint did not reject")
	}
	issued := httpIssue(teacherSession, `{"role":"student"}`)
	if issued.Code != http.StatusCreated {
		t.Fatal("teacher invitation HTTP endpoint failed")
	}
	var issuedBody struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(issued.Body.Bytes(), &issuedBody) != nil || issuedBody.Code == "" {
		t.Fatal("invitation code missing from one-time response")
	}
	registerBody := fmt.Sprintf(`{"code":%q,"username":%q,"password":"registration-http-test-password"}`, issuedBody.Code, tag+"-http-student")
	request := httptest.NewRequest(http.MethodPost, "/api/register", strings.NewReader(registerBody))
	request.Header.Set("Origin", "http://registration.test")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatal("registration HTTP route did not create account")
	}
	var returned repositories.User
	if json.Unmarshal(response.Body.Bytes(), &returned) != nil || returned.Role != "student" || returned.ClassID != classID {
		t.Fatal("registration HTTP response identity did not match invite")
	}
	var sessionCookie *http.Cookie
	for _, c := range response.Result().Cookies() {
		if c.Name == "campusclaw_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly {
		t.Fatal("registration did not establish HttpOnly session")
	}
	if me, err := auth.Me(ctx, sessionCookie.Value); err != nil || me.ID != returned.ID || me.ClassID != classID || me.Role != "student" {
		t.Fatal("registration session did not resolve the created account")
	}
	studentIssue := httpIssue(sessionCookie.Value, `{"role":"student"}`)
	if studentIssue.Code != http.StatusForbidden {
		t.Fatal("student HTTP invitation endpoint did not reject")
	}
	if w := httpIssue(teacherSession, `{"role":"student","class_id":99999}`); w.Code != http.StatusBadRequest {
		t.Fatal("client class override was not rejected")
	}
	apiInvite, apiCode, err := store.Issue(ctx, teacher, "student")
	if err != nil {
		t.Fatal("API injection fixture setup failed")
	}
	forged := fmt.Sprintf(`{"code":%q,"username":%q,"password":"registration-http-test-password","role":"teacher","class_id":99999}`, apiCode, tag+"-forged")
	forgedRequest := httptest.NewRequest(http.MethodPost, "/api/register", strings.NewReader(forged))
	forgedRequest.Header.Set("Origin", "http://registration.test")
	forgedRequest.Header.Set("Content-Type", "application/json")
	forgedResponse := httptest.NewRecorder()
	app.ServeHTTP(forgedResponse, forgedRequest)
	if forgedResponse.Code != http.StatusBadRequest {
		t.Fatal("registration role/class injection was not rejected")
	}
	if err = store.Register(ctx, apiCode, tag+"-after-injection", "registration-http-test-password"); err != nil {
		t.Fatal("rejected injection consumed the invitation")
	}
	_ = apiInvite
	fmt.Println("CHECK: invitation hashes, both roles, class binding, invalid/duplicate/replay/expiry/revocation denial, role/class injection denial and concurrent single consumption passed")
}
