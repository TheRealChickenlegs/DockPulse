<script lang="ts">
	import { onMount } from 'svelte';
	import { animateOn } from '$animations';
	import { api, ApiError } from '$lib/api';
	import { session } from '$lib/stores';
	import { resolve } from '$app/paths';

	let serverName = $state('');
	let ttlHours = $state(24);
	let busy = $state(false);
	let error: string | null = $state(null);
	let issued: { token: string; expires_at: string; ca_fingerprint: string; server_name: string } | null = $state(null);
	let copied = $state(false);

	onMount(() => {
		animateOn('.panel', { opacity: [0, 1], y: [8, 0] }, { duration: 0.4, easing: 'ease-out' });
	});

	async function generate() {
		error = null;
		busy = true;
		issued = null;
		copied = false;
		try {
			issued = await api.createEnrollmentToken({
				server_name: serverName,
				ttl_hours: ttlHours
			});
		} catch (err) {
			if (err instanceof ApiError) {
				error = err.message;
			} else {
				error = 'Failed to create token';
			}
		} finally {
			busy = false;
		}
	}

	async function copy() {
		if (!issued) return;
		await navigator.clipboard.writeText(issued.token);
		copied = true;
		setTimeout(() => (copied = false), 1500);
	}

	function agentCommand(): string {
		if (!issued) return '';
		return `# On the new agent host, save the token and start the agent:
cat > agent-data/token <<'EOF'
${issued.token}
EOF
chmod 600 agent-data/token

# Pass the controller's CA fingerprint along (optional pin).
docker run --rm -i \\
  -v "$PWD/agent-data:/data" \\
  -v /var/run/docker.sock:/var/run/docker.sock:ro \\
  ghcr.io/therealchickenlegs/dockpulse:edge \\
  --mode=agent \\
  --name=${issued.server_name} \\
  --controller=https://dockpulse.example.com \\
  --controller-ca=/data/agent-ca.crt \\
  --enroll-token-file=/data/token \\
  --data=/data`;
	}
</script>

<section class="page">
	<header>
		<h1>Add an agent</h1>
		<p class="muted">
			Generate a one-time enrollment token. The token is shown once and only its SHA-256 hash is
			stored in the database.
		</p>
		<a class="back" href={resolve('/servers')}>← Back to servers</a>
	</header>

	{#if $session.user?.role !== 'admin'}
		<p class="error">Only admin users can create enrollment tokens.</p>
	{:else}
		<div class="panel">
			<form onsubmit={(e) => { e.preventDefault(); generate(); }}>
				<label>
					<span>Server name</span>
					<input
						type="text"
						bind:value={serverName}
						required
						maxlength="64"
						placeholder="server-a"
					/>
				</label>
				<label>
					<span>Token lifetime (hours)</span>
					<input type="number" bind:value={ttlHours} min="1" max="168" required />
				</label>
				{#if error}
					<p class="error">{error}</p>
				{/if}
				<button type="submit" disabled={busy}>{busy ? 'Generating…' : 'Generate token'}</button>
			</form>

			{#if issued}
				<aside class="issued">
					<h2>Token issued</h2>
					<p class="muted">
						Token expires at <code>{new Date(issued.expires_at).toLocaleString()}</code>. The
						controller's CA fingerprint is <code>{issued.ca_fingerprint}</code>.
					</p>
					<div class="token-row">
						<code class="token">{issued.token}</code>
						<button type="button" onclick={copy}>{copied ? 'Copied!' : 'Copy'}</button>
					</div>
					<details>
						<summary>Docker run command</summary>
						<pre>{agentCommand()}</pre>
					</details>
				</aside>
			{/if}
		</div>
	{/if}
</section>

<style>
	.page {
		max-width: 720px;
	}
	header {
		margin-bottom: 1.25rem;
	}
	header h1 {
		margin: 0 0 0.3rem;
	}
	.muted {
		color: var(--fg-1);
	}
	.back {
		display: inline-block;
		margin-top: 0.5rem;
		font-size: 0.85rem;
		color: var(--fg-1);
		text-decoration: none;
	}
	.back:hover {
		color: var(--fg-0);
	}
	.panel {
		background: var(--bg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
		padding: 1.25rem 1.4rem;
		opacity: 0;
	}
	form {
		display: grid;
		gap: 0.9rem;
	}
	label {
		display: grid;
		gap: 0.3rem;
	}
	label span {
		font-size: 0.85rem;
		color: var(--fg-1);
	}
	input {
		padding: 0.55rem 0.7rem;
		background: var(--bg-2);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		color: var(--fg-0);
		font: inherit;
	}
	input:focus {
		outline: 2px solid var(--accent-1);
		outline-offset: 1px;
	}
	button[type='submit'] {
		justify-self: start;
		padding: 0.55rem 1.1rem;
		background: var(--accent-grad);
		color: #0b0f17;
		border: 0;
		border-radius: var(--radius-sm);
		font-weight: 600;
		cursor: pointer;
	}
	button[type='submit']:disabled {
		opacity: 0.6;
		cursor: progress;
	}
	.issued {
		margin-top: 1.25rem;
		padding-top: 1.25rem;
		border-top: 1px solid var(--border);
	}
	.issued h2 {
		margin: 0 0 0.3rem;
		font-size: 1rem;
	}
	.token-row {
		display: flex;
		gap: 0.5rem;
		align-items: center;
		margin: 0.6rem 0;
	}
	.token {
		flex: 1;
		padding: 0.55rem 0.7rem;
		background: var(--bg-2);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		font-family: var(--font-mono);
		font-size: 0.85rem;
		word-break: break-all;
	}
	.token-row button {
		padding: 0.4rem 0.7rem;
		border-radius: var(--radius-sm);
		background: var(--bg-2);
		border: 1px solid var(--border);
		color: var(--fg-0);
		cursor: pointer;
	}
	details {
		margin-top: 0.5rem;
	}
	details pre {
		padding: 0.7rem 0.8rem;
		background: var(--bg-2);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		font-size: 0.8rem;
		overflow-x: auto;
	}
	.error {
		color: var(--danger);
	}
	code {
		font-family: var(--font-mono);
	}
</style>
