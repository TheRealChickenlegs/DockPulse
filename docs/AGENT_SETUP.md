# Agent Setup

> Detailed agent setup guide will be expanded in Phase 1 when the enrollment flow is implemented. Phase 0 only includes the deployable compose file stub.

## At a glance (Phase 0)

The agent compose file is `deploy/docker-compose.agent.yml`. It currently runs the agent in `--mode=agent` against a placeholder controller URL. Until the enrollment API is implemented, the agent will fail to connect and log an error — this is expected.

In Phase 1 the workflow will be:

1. In the controller UI, go to **Admin → Agents → New enrollment token**.
2. Copy the token.
3. On the agent host, drop the token into `deploy/agent-data/token`.
4. `docker compose -f deploy/docker-compose.agent.yml up -d`.
5. The agent enrolls, receives a client cert, and starts reporting.