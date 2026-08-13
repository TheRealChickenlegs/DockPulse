package agentapi

import (
	"database/sql"
	"net/http"
	"sync"

	"github.com/TheRealChickenlegs/DockPulse/go/internal/controller/auth"
)

// AgentCommand is one command for a single server's agent, drained
// by GET /agent/v1/commands/poll.
type AgentCommand struct {
	Type string `json:"type"`
}

// CommandQueue is an in-memory, per-server queue of commands for the
// agents. In-memory is sufficient because the controller is a single
// instance and commands are ephemeral: a scan request lost on
// restart is harmless (the agent re-polls on its own schedule).
type CommandQueue struct {
	mu sync.Mutex
	q  map[string][]AgentCommand
}

// NewCommandQueue constructs an empty queue.
func NewCommandQueue() *CommandQueue {
	return &CommandQueue{q: map[string][]AgentCommand{}}
}

// Enqueue appends a command for the given server.
func (cq *CommandQueue) Enqueue(serverID, commandType string) {
	cq.mu.Lock()
	defer cq.mu.Unlock()
	cq.q[serverID] = append(cq.q[serverID], AgentCommand{Type: commandType})
}

// Drain returns and clears the pending commands for a server.
func (cq *CommandQueue) Drain(serverID string) []AgentCommand {
	cq.mu.Lock()
	defer cq.mu.Unlock()
	cmds := cq.q[serverID]
	delete(cq.q, serverID)
	return cmds
}

// HandleCommandsPoll drains the server's command queue. Agents poll
// this on a short interval so a UI-initiated scan is picked up
// quickly; the controller never needs inbound access to the agent.
func (s *Server) HandleCommandsPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	serverID := ServerIDFromContext(r.Context())
	if serverID == "" {
		http.Error(w, "missing server context", http.StatusInternalServerError)
		return
	}
	cmds := s.Commands.Drain(serverID)
	if cmds == nil {
		cmds = []AgentCommand{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": cmds})
}

// HandleRequestScan enqueues a scan command for a server's agent.
// Any authenticated user may trigger a re-scan; the action is
// audit-logged. The endpoint requires CSRF (enforced by the auth
// middleware group it is registered under).
func HandleRequestScan(db *sql.DB, queue *CommandQueue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		serverID := r.PathValue("id")
		if serverID == "" {
			http.Error(w, "server id required", http.StatusBadRequest)
			return
		}
		var exists bool
		if err := db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM servers WHERE id = ?)`, serverID).Scan(&exists); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !exists {
			http.Error(w, "server not found", http.StatusNotFound)
			return
		}
		queue.Enqueue(serverID, "scan")
		if u, ok := auth.UserFrom(r.Context()); ok {
			_, _ = db.ExecContext(r.Context(), `
				INSERT INTO audit_log(actor_kind, actor_id, action, target)
				VALUES ('user', ?, 'server.scan.request', ?)
			`, u.ID, serverID)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
