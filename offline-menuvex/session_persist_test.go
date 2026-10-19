package desktop

import (
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// نشست ورود باید روی دیسک بماند تا با اجرای دوباره برنامه، کاربر مجبور
// به ورود مجدد نباشد؛ خروج صریح آن را پاک می‌کند.
func TestSessionPersistsAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	a1 := &Application{sessionsPath: path, sessions: loadSessionsFile(path)}
	rec := httptest.NewRecorder()
	a1.newSession(rec, "manager", "مدیر")
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("session cookie not set")
	}
	token := cookies[0].Value

	// شبیه‌سازی اجرای دوباره برنامه: خواندن از همان فایل
	a2 := &Application{sessionsPath: path, sessions: loadSessionsFile(path)}
	req := httptest.NewRequest("GET", "http://localhost/", nil)
	req.AddCookie(cookies[0])
	if info, ok := a2.sessionOf(req); !ok || info.Role != "manager" {
		t.Fatalf("session must survive restart, got ok=%v role=%q token=%s", ok, info.Role, token[:6])
	}

	// خروج باید نشست را از دیسک هم پاک کند
	rec2 := httptest.NewRecorder()
	a2.dropSession(rec2, req)
	a3 := &Application{sessionsPath: path, sessions: loadSessionsFile(path)}
	if _, ok := a3.sessionOf(req); ok {
		t.Fatal("session must be gone after logout")
	}
}
