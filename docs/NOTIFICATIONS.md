# Notifiers

> Pluggable outbound notifications. Implemented in Phase 4.

## Supported kinds

| Kind | Phase | Auth | Notes |
| --- | --- | --- | --- |
| `discord` | 4 | Webhook URL | Outbound HTTPS POST with embedded JSON body. |
| `slack` | 4 | Webhook URL | Compatible with Slack incoming webhooks. |
| `ntfy` | 4 | Topic + server (optional auth token) | Public or self-hosted ntfy.sh. |
| `email` | 4 | SMTP | STARTTLS enforced; auth required for non-localhost relays. |
| `telegram` | 4 | Bot token + chat ID | Via the official Bot HTTP API. |

## Events

| Event | Trigger |
| --- | --- |
| `update.available` | A new image digest is detected for a running container. |
| `update.applied` | An admin-initiated apply completed successfully on the agent. |
| `agent.offline` | No heartbeat from an agent within the configured grace window. |
| `agent.enrolled` | A new agent completed enrollment. |
| `agent.revoked` | An admin revoked an agent's certificate. |

## Per-user preferences

Each user toggles which events go to which notifiers. A simple admin "test" button sends a synthetic event and surfaces the result inline.