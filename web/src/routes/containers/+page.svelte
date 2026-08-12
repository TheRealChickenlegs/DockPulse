<script lang="ts">
	import { onMount } from 'svelte';
	import { resolve } from '$app/paths';
	import { api, type ServerListItem, type ContainerListItem } from '$lib/api';

	let servers: ServerListItem[] = $state([]);
	let selectedId: string = $state('');
	let containers: ContainerListItem[] = $state([]);
	let loadingServers = $state(true);
	let loadingContainers = $state(false);
	let error: string | null = $state(null);
	let selected: ContainerListItem | null = $state(null);

	async function loadServers() {
		loadingServers = true;
		error = null;
		try {
			const res = await api.listServers();
			servers = res.servers;
			if (servers.length > 0 && !selectedId) {
				selectedId = servers[0].id;
				await loadContainers(selectedId);
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load servers';
		} finally {
			loadingServers = false;
		}
	}

	async function loadContainers(id: string) {
		loadingContainers = true;
		try {
			const res = await api.listContainers(id);
			containers = res.containers;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load containers';
			containers = [];
		} finally {
			loadingContainers = false;
		}
	}

	$effect(() => {
		if (selectedId) loadContainers(selectedId);
	});

	function open(c: ContainerListItem) {
		selected = c;
	}
	function close() {
		selected = null;
	}
	function onKey(e: KeyboardEvent) {
		if (e.key === 'Escape') close();
	}

	onMount(() => {
		loadServers();
		const id = setInterval(loadServers, 60_000);
		window.addEventListener('keydown', onKey);
		return () => {
			clearInterval(id);
			window.removeEventListener('keydown', onKey);
		};
	});

	function stateColor(state: string) {
		switch (state) {
			case 'running':
				return 'var(--ok)';
			case 'exited':
				return 'var(--fg-1)';
			case 'paused':
				return 'var(--warn)';
			case 'restarting':
				return 'var(--warn)';
			default:
				return 'var(--fg-1)';
		}
	}
</script>

<section class="wrap">
	<aside class="rail" aria-label="server list">
		<header>
			<h2>Servers</h2>
			<a class="muted-link" href={resolve('/servers')}>All servers →</a>
		</header>
		{#if loadingServers}
			<p class="muted">Loading…</p>
		{:else if servers.length === 0}
			<p class="muted">No servers enrolled.</p>
		{:else}
			<ul>
				{#each servers as server (server.id)}
					<li>
						<button
							type="button"
							class="server"
							class:active={selectedId === server.id}
							onclick={() => (selectedId = server.id)}
						>
							<span class="dot" style="background:{stateColor(server.status)}"></span>
							<span class="name">{server.name}</span>
							<span class="count">{server.running_count}/{server.container_count}</span>
						</button>
					</li>
				{/each}
			</ul>
		{/if}
	</aside>

	<section class="pane">
		<header>
			<h1>{servers.find((s) => s.id === selectedId)?.name ?? 'Containers'}</h1>
			<p class="muted">
				{#if selectedId}
					{containers.length} container{containers.length === 1 ? '' : 's'} · updates and apply land in Phase 2+
				{:else}
					Pick a server on the left to see its containers.
				{/if}
			</p>
		</header>

		{#if error}
			<p class="error">{error}</p>
		{:else if loadingContainers}
			<p class="muted">Loading…</p>
		{:else if containers.length === 0}
			<p class="muted">No containers cached for this server yet. The agent reports every 2 minutes.</p>
		{:else}
			<div class="grid">
				{#each containers as c (c.id)}
					<button type="button" class="card" onclick={() => open(c)}>
						<header>
							<span class="dot" style="background:{stateColor(c.state)}"></span>
							<span class="name">{c.name}</span>
						</header>
						<code class="image">{c.image_ref}</code>
						<div class="state">{c.state}</div>
					</button>
				{/each}
			</div>
		{/if}
	</section>
</section>

{#if selected}
	<div class="drawer-backdrop" onclick={close} role="presentation"></div>
	<div class="drawer" role="dialog" aria-modal="true" aria-label={selected.name}>
		<header>
			<h2>{selected.name}</h2>
			<button class="close" type="button" onclick={close} aria-label="Close">×</button>
		</header>
		<dl>
			<dt>State</dt>
			<dd>
				<span class="dot" style="background:{stateColor(selected.state)}"></span>
				{selected.state}
			</dd>
			<dt>Image</dt>
			<dd><code>{selected.image_ref}</code></dd>
			<dt>Local digest</dt>
			<dd><code>{selected.image_digest_local || '—'}</code></dd>
			<dt>Started</dt>
			<dd>{selected.started_at ? new Date(selected.started_at).toLocaleString() : '—'}</dd>
			<dt>Docker ID</dt>
			<dd><code>{selected.docker_id}</code></dd>
			<dt>Updated</dt>
			<dd>{new Date(selected.updated_at).toLocaleString()}</dd>
		</dl>
		<p class="muted">Changelog and apply-update actions land in Phase 2+.</p>
	</div>
{/if}

<style>
	.wrap {
		display: grid;
		grid-template-columns: 240px 1fr;
		gap: 1.5rem;
		min-height: 60vh;
	}
	.rail {
		background: var(--bg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
		padding: 0.8rem;
	}
	.rail header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 0.7rem;
	}
	.rail h2 {
		margin: 0;
		font-size: 0.95rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--fg-1);
	}
	.rail ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 0.2rem;
	}
	.server {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		width: 100%;
		padding: 0.5rem 0.6rem;
		background: transparent;
		border: 0;
		border-radius: var(--radius-sm);
		color: var(--fg-0);
		font: inherit;
		cursor: pointer;
		text-align: left;
	}
	.server:hover {
		background: var(--bg-2);
	}
	.server.active {
		background: var(--bg-2);
		color: var(--fg-0);
	}
	.server .name {
		flex: 1;
	}
	.server .count {
		font-family: var(--font-mono);
		font-size: 0.78rem;
		color: var(--fg-1);
	}
	.muted {
		color: var(--fg-1);
		font-size: 0.85rem;
	}
	.muted-link {
		color: var(--fg-1);
		font-size: 0.78rem;
		text-decoration: none;
	}
	.muted-link:hover {
		color: var(--fg-0);
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--fg-1);
		display: inline-block;
	}
	.pane header {
		margin-bottom: 1rem;
	}
	.pane h1 {
		margin: 0 0 0.2rem;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
		gap: 0.8rem;
	}
	.card {
		text-align: left;
		background: var(--bg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
		padding: 0.9rem 1rem;
		color: inherit;
		cursor: pointer;
		transition: transform 160ms ease, border-color 160ms ease;
	}
	.card:hover {
		border-color: rgba(129, 140, 248, 0.5);
		transform: translateY(-1px);
	}
	.card header {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		margin-bottom: 0.4rem;
	}
	.card .name {
		font-weight: 600;
	}
	.card .image {
		font-family: var(--font-mono);
		font-size: 0.78rem;
		color: var(--fg-1);
		display: block;
		word-break: break-all;
	}
	.card .state {
		margin-top: 0.5rem;
		font-size: 0.78rem;
		color: var(--fg-1);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.error {
		color: var(--danger);
		padding: 1rem 1.25rem;
		background: var(--bg-1);
		border: 1px solid rgba(248, 113, 113, 0.3);
		border-radius: var(--radius-md);
	}

	.drawer-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0, 0, 0, 0.45);
		z-index: 30;
		animation: fade 0.2s ease;
	}
	.drawer {
		position: fixed;
		top: 0;
		right: 0;
		bottom: 0;
		width: min(420px, 100vw);
		background: var(--bg-1);
		border-left: 1px solid var(--border);
		padding: 1.1rem 1.25rem;
		overflow-y: auto;
		z-index: 31;
		animation: slideIn 0.25s cubic-bezier(0.2, 0.8, 0.2, 1);
		box-shadow: var(--shadow-md);
	}
	.drawer header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 1rem;
	}
	.drawer h2 {
		margin: 0;
	}
	.drawer .close {
		background: transparent;
		border: 0;
		color: var(--fg-1);
		font-size: 1.5rem;
		line-height: 1;
		cursor: pointer;
	}
	.drawer .close:hover {
		color: var(--fg-0);
	}
	dl {
		margin: 0 0 1rem;
		display: grid;
		grid-template-columns: 130px 1fr;
		gap: 0.5rem 0.75rem;
		font-size: 0.9rem;
	}
	dt {
		color: var(--fg-1);
	}
	dd {
		margin: 0;
	}
	dd code {
		font-family: var(--font-mono);
		font-size: 0.83rem;
		word-break: break-all;
	}
	@keyframes fade {
		from {
			opacity: 0;
		}
		to {
			opacity: 1;
		}
	}
	@keyframes slideIn {
		from {
			transform: translateX(20px);
			opacity: 0;
		}
		to {
			transform: translateX(0);
			opacity: 1;
		}
	}
</style>
