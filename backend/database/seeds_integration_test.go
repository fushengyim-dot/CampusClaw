package database

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"golang.org/x/crypto/bcrypt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSeedIntegration(t *testing.T) {
	if os.Getenv("CAMPUSCLAW_SEED_INTEGRATION") != "1" {
		t.Skip("explicit account test required")
	}
	c := integrationConfig(t)
	if c.MySQLDatabase != "campusclaw_course" || c.MySQLUser != "campusclaw" {
		t.Fatal("unexpected project scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tables := append(append([]string{}, schemaTables...), "schema_migrations", "connection_check_probe")
	baseline := schemaSnapshot(t, ctx, db, tables)
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		t.Fatal("test random failed")
	}
	tag := "seed-check-" + hex.EncodeToString(random)
	baseID := int64(binary.BigEndian.Uint32(random)) + 10000000000
	classes := []seedClass{{baseID, tag + "-A"}, {baseID + 1, tag + "-B"}}
	accounts := make([]seedAccount, len(demoAccounts))
	copy(accounts, demoAccounts)
	for i := range accounts {
		accounts[i].username = tag + accounts[i].username
		accounts[i].classID = baseID + (accounts[i].classID - 1)
	}
	passwords := map[string]string{}
	for _, a := range accounts {
		passwords[a.variable] = tag + a.role
	}
	passwords[accounts[0].variable] = strings.Repeat("界", 24) // Exactly 72 bytes.
	get := func(k string) string { return passwords[k] }
	for _, tc := range []struct{ name, value string }{{"missing", ""}, {"blank", " \t"}, {"over72bytes", strings.Repeat("界", 25)}} {
		t.Run(tc.name, func(t *testing.T) {
			tx, e := db.BeginTx(ctx, nil)
			if e != nil {
				t.Fatal("begin failed")
			}
			defer tx.Rollback()
			getter := func(k string) string {
				if k == accounts[3].variable {
					return tc.value
				}
				return get(k)
			}
			e = seedAccountsTx(ctx, tx, getter, classes, accounts)
			if e == nil || !strings.Contains(e.Error(), accounts[3].variable) {
				t.Fatal("invalid final password not rejected")
			}
			if tx.Rollback() != nil {
				t.Fatal("rollback failed")
			}
			if !reflect.DeepEqual(baseline, schemaSnapshot(t, ctx, db, tables)) {
				t.Fatal("failed seed left partial rows")
			}
		})
	}
	t.Run("bcrypt_repeat_changed_password_and_conflict", func(t *testing.T) {
		tx, e := db.BeginTx(ctx, nil)
		if e != nil {
			t.Fatal("begin failed")
		}
		defer tx.Rollback()
		if e = seedAccountsTx(ctx, tx, get, classes, accounts); e != nil {
			t.Fatal(e)
		}
		saved := map[string]string{}
		for _, a := range accounts {
			var hash string
			if tx.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE username=?", a.username).Scan(&hash) != nil {
				t.Fatal("account absent")
			}
			cost, e := bcrypt.Cost([]byte(hash))
			if e != nil || cost != 12 || bcrypt.CompareHashAndPassword([]byte(hash), []byte(get(a.variable))) != nil {
				t.Fatal("bcrypt validation failed")
			}
			saved[a.username] = hash
		}
		// Simulate a password changed after initialization on a synthetic account only.
		changedPassword := tag + "-changed"
		changedHash, e := bcrypt.GenerateFromPassword([]byte(changedPassword), 12)
		if e != nil {
			t.Fatal("test hash failed")
		}
		if _, e = tx.ExecContext(ctx, "UPDATE users SET password_hash=? WHERE username=?", string(changedHash), accounts[0].username); e != nil {
			t.Fatal("synthetic password change failed")
		}
		saved[accounts[0].username] = string(changedHash)
		if e = seedAccountsTx(ctx, tx, func(string) string { return "" }, classes, accounts); e != nil {
			t.Fatal("existing accounts required seed passwords")
		}
		if e = seedAccountsTx(ctx, tx, func(string) string { return "different-test-input" }, classes, accounts); e != nil {
			t.Fatal(e)
		}
		for _, a := range accounts {
			var hash string
			if tx.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE username=?", a.username).Scan(&hash) != nil || hash != saved[a.username] {
				t.Fatal("existing hash overwritten")
			}
		}
		var changedStored string
		if tx.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE username=?", accounts[0].username).Scan(&changedStored) != nil || bcrypt.CompareHashAndPassword([]byte(changedStored), []byte(changedPassword)) != nil || bcrypt.CompareHashAndPassword([]byte(changedStored), []byte(get(accounts[0].variable))) == nil {
			t.Fatal("changed password was reset")
		}
		// Conflicting names/roles are refused, never repaired by overwriting.
		conflict := append([]seedAccount{}, accounts...)
		conflict[0].role = "student"
		if seedAccountsTx(ctx, tx, get, classes, conflict) == nil {
			t.Fatal("role conflict accepted")
		}
		classConflict := append([]seedClass{}, classes...)
		classConflict[0].name = "different"
		if seedAccountsTx(ctx, tx, get, classConflict, accounts) == nil {
			t.Fatal("class conflict accepted")
		}
		if tx.Rollback() != nil {
			t.Fatal("cleanup rollback failed")
		}
	})
	if !reflect.DeepEqual(baseline, schemaSnapshot(t, ctx, db, tables)) {
		t.Fatal("synthetic checks changed existing rows")
	}
	// Actual seeded accounts must already exist; this check must not silently create them.
	for _, a := range demoAccounts {
		var hash, role string
		var classID int64
		if db.QueryRowContext(ctx, "SELECT password_hash,role,class_id FROM users WHERE username=?", a.username).Scan(&hash, &role, &classID) != nil || role != a.role || classID != a.classID {
			t.Fatal("real demo account missing or incorrectly scoped")
		}
		cost, e := bcrypt.Cost([]byte(hash))
		if e != nil || cost != 12 {
			t.Fatal("real password cost invalid")
		}
		if os.Getenv(a.variable) == "" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(os.Getenv(a.variable))) != nil {
			t.Fatal("saved initial credential does not match; may have been changed, no reset performed")
		}
	}
	if err = InitAccounts(ctx, db, func(string) string { return "different-test-input" }); err != nil {
		t.Fatal(err)
	}
	if err = InitAccounts(ctx, db, func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- InitAccounts(ctx, db, func(string) string { return "" }) }()
	}
	for i := 0; i < 2; i++ {
		if <-done != nil {
			t.Fatal("concurrent seed failed")
		}
	}
	if !reflect.DeepEqual(baseline, schemaSnapshot(t, ctx, db, tables)) {
		t.Fatal("repeat or concurrent seed changed rows")
	}
	var classCount, userCount int
	if db.QueryRowContext(ctx, "SELECT COUNT(*) FROM classes WHERE id IN (1,2)").Scan(&classCount) != nil || classCount != 2 {
		t.Fatal("class count mismatch")
	}
	if db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE username IN ('teacher_a','student_a1','teacher_b','student_b1')").Scan(&userCount) != nil || userCount != 4 {
		t.Fatal("account count mismatch")
	}
	t.Log("PASS: two classes/four accounts, bcrypt cost12 and private credentials verified; repeat/new input/no input/concurrent execution preserved all rows")
	t.Log("PASS: missing/blank/over72-byte password rollback; changed synthetic password preserved; conflicts refused; all test rows rolled back")
}
