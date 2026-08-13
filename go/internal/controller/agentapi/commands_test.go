package agentapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/auth"
)

func TestCommandQueueDrain(t *testing.T) {
	q := NewCommandQueue()
	if got := q.Drain("s1"); got != nil {
		t.Fatalf("Drain on empty queue = %+v, want nil", got)
	}
	q.Enqueue("s1", "scan")
	q.Enqueue("s1", "scan")
	q.Enqueue("s2", "scan")
	got := q.Drain("s1")
	if len(got) != 2 {
		t.Fatalf("Drain(s1) len = %d, want 2", len(got))
	}
	if got[0].Type != "scan" || got[1].Type != "scan" {
		t.Errorf("commands = %+v", got)
	}
	if left := q.Drain("s1"); len(left) != 0 {
		t.Errorf("second Drain(s1) = %+v, want empty", left)
	}
	if left := q.Drain("s2"); len(left) != 1 {
		t.Errorf("Drain(s2) = %+v, want 1 command", left)
	}
}

func TestHandleCommandsPoll(t *testing.T) {
	s, _, _ := newTestServer(t)
	s.Commands.Enqueue("server-a", "scan")

	r := httptest.NewRequest(http.MethodGet, "/agent/v1/commands/poll", nil)
	r = r.WithContext(AttachServerID(r.Context(), "server-a"))
	w := httptest.NewRecorder()
	s.HandleCommandsPoll(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("poll: %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Commands []AgentCommand `json:"commands"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Commands) != 1 || payload.Commands[0].Type != "scan" {
		t.Fatalf("commands = %+v, want one scan", payload.Commands)
	}
}

func TestHandleCommandsPollIsPerServer(t *testing.T) {
	s, _, _ := newTestServer(t)
	s.Commands.Enqueue("server-a", "scan")

	r := httptest.NewRequest(http.MethodGet, "/agent/v1/commands/poll", nil)
	r = r.WithContext(AttachServerID(r.Context(), "server-b"))
	w := httptest.NewRecorder()
	s.HandleCommandsPoll(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("poll: %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Commands []AgentCommand `json:"commands"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Commands) != 0 {
		t.Errorf("other server poll = %+v, want empty commands", payload.Commands)
	}
}

func TestHandleRequestScan(t *testing.T) {
	s, d, ca := newTestServer(t)
	admin := makeUser(t, d, "admin", "admin")
	tok := makeToken(t, d, admin.ID, "server-a")
	_, serverID := enroll(t, s, tok, "server-a", ca.Fingerprint())

	h := HandleRequestScan(s.DB, s.Commands)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/servers/"+serverID+"/refresh", nil)
	r.SetPathValue("id", serverID)
	r = r.WithContext(auth.ContextWithUser(r.Context(), admin))
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", w.Code, w.Body.String())
	}
	cmds := s.Commands.Drain(serverID)
	if len(cmds) != 1 || cmds[0].Type != "scan" {
		t.Fatalf("commands = %+v, want one scan", cmds)
	}
}

func TestHandleRequestScanMissingServer(t *testing.T) {
	s, _, _ := newTestServer(t)
	h := HandleRequestScan(s.DB, s.Commands)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/servers/nope/refresh", nil)
	r.SetPathValue("id", "nope")
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("refresh unknown server: %d, want 404", w.Code)
	}
	if len(s.Commands.Drain("nope")) != 0 {
		t.Fatal("command must not be enqueued for a missing server")
	}
}
