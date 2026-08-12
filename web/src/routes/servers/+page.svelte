<script lang="ts">
	import { onMount } from 'svelte';
	import { resolve } from '$app/paths';
	import { api, type ServerListItem } from '$lib/api';
	import { session } from '$lib/stores';

	let servers: ServerListItem[] = $state([]);
	let selectedId: string = $state('');
	let loading = $state(true);
	let error: string | null = $state(null);

	async function refresh() {
		loading = true;
		error = null;
		try {
			const res = await api.listServers();
			servers = res.servers;
			if (servers.length > 0 && !selectedId) {
				selectedId = servers[0].id;
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load servers';
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		refresh();
		const id = setInterval(refresh, 60_000);
		return () => clearInterval(id);
	});
</script>

<section class="placeholder">
	<header class="head">
		<div>
			<h1>Servers</h1>
			<p class="hint">All Docker hosts running the DockPulse agent.</p>
		</div>
		{#if $session.user?.role === 'admin'}
			<a class="cta" href={resolve('/admin/agents')}>Add agent</a>
		{/if}
	</header>

	{#if loading && servers.length === 0}
		<p class="empty">Loading…</p>
	{:else if error}
		<p class="error">{error}</p>
	{:else if servers.length === 0}
		<p class="empty">No agents are enrolled yet. Generate an enrollment token from the admin page and run the agent on a host.</p>
	{:else}
		<div class="grid">
			{#each servers as server (server.id)}
				<button
					type="button"
					class="card"
					class:selected={selectedId === server.id}
					onclick={() => (selectedId = server.id)}
				>
					<div class="card-head">
						<div class="card-title">
							<span class="dot" data-status={server.status}></span>
							{server.name}
						</div>
						<div class="card-meta">
							{server.container_count} container{server.container_count === 1 ? '' : 's'} ·
							{server.running_count} running
						</div>
					</div>
					<div class="card-detail">
						{#if server.hostname}<div>host: <code>{server.hostname}</code></div>{/if}
						{#if server.os}<div>os: <code>{server.os}</code></div>{/if}
						{#if server.docker_version}<div>docker: <code>{server.docker_version}</code></div>{/if}
						{#if server.last_seen_at}
							<div>last seen: <code>{new Date(server.last_seen_at).toLocaleString()}</code></div>
						{:else}
							<div>last seen: <em>never</em></div>
						{/if}
					</div>
				</button>
			{/each}
		</div>
	{/if}
</section>

<style>
	.placeholder {
		display: flex;
		flex-direction: column;
		gap: 1.25rem;
	}
	.head {
		display: flex;
		align-items: flex-end;
		justify-content: space-between;
		gap: 1rem;
	}
	h1 {
		margin: 0 0 0.3rem;
	}
	.hint {
		color: var(--fg-1);
		margin: 0;
	}
	.cta {
		padding: 0.5rem 0.9rem;
		border-radius: var(--radius-sm);
		background: var(--accent-grad);
		color: #0b0f17;
		font-weight: 600;
		text-decoration: none;
	}
	.empty,
	.error {
		color: var(--fg-1);
		padding: 1rem 1.25rem;
		background: var(--bg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
	}
	.error {
		color: var(--danger);
		border-color: rgba(248, 113, 113, 0.3);
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
		gap: 0.9rem;
	}
	.card {
		text-align: left;
		background: var(--bg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
		padding: 1rem 1.1rem;
		color: inherit;
		cursor: pointer;
		transition: transform 160ms ease, border-color 160ms ease, background-color 160ms ease;
	}
	.card:hover {
		border-color: rgba(129, 140, 248, 0.5);
	}
	.card.selected {
		border-color: var(--accent-1);
		box-shadow: 0 0 0 1px var(--accent-1) inset;
	}
	.card-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
	}
	.card-title {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-weight: 600;
	}
	.card-meta {
		font-size: 0.8rem;
		color: var(--fg-1);
	}
	.card-detail {
		margin-top: 0.6rem;
		font-size: 0.83rem;
		color: var(--fg-1);
		display: grid;
		gap: 0.2rem;
	}
	.card-detail code {
		font-family: var(--font-mono);
	}
	.dot {
		width: 9px;
		height: 9px;
		border-radius: 50%;
		background: var(--fg-1);
		display: inline-block;
	}
	.dot[data-status='online'] {
		background: var(--ok);
	}
	.dot[data-status='offline'] {
		background: var(--fg-1);
		opacity: 0.5;
	}
	.dot[data-status='pending'] {
		background: var(--warn);
	}
	.dot[data-status='revoked'] {
		background: var(--danger);
	}
</style>
