<script lang="ts">
	import { onMount } from 'svelte';
	import { animateOn } from '$animations';
	import { api, type UpdateListItem } from '$lib/api';

	let updates: UpdateListItem[] = $state([]);
	let loading = $state(true);
	let error: string | null = $state(null);

	async function load() {
		loading = true;
		error = null;
		try {
			const res = await api.listUpdates();
			updates = res.updates;
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load updates';
			updates = [];
		} finally {
			loading = false;
		}
	}

	function shortDigest(d: string) {
		if (!d) return '—';
		return d.length > 19 ? d.slice(0, 19) + '…' : d;
	}

	$effect(() => {
		if (!loading) {
			animateOn('.card', { opacity: [0, 1], y: [8, 0] }, { duration: 0.4, easing: 'ease-out' });
		}
	});

	onMount(() => {
		load();
		const id = setInterval(load, 60_000);
		return () => clearInterval(id);
	});

	function statusColor(status: string) {
		switch (status) {
			case 'pending':
				return 'var(--warn)';
			case 'applied':
				return 'var(--ok)';
			default:
				return 'var(--fg-1)';
		}
	}
</script>

<svelte:head>
	<title>Updates — DockPulse</title>
</svelte:head>

<header>
	<h1>Updates</h1>
	<p class="muted">
		{#if updates.length > 0}
			{updates.length} detected update{updates.length === 1 ? '' : 's'} · the agent polls
			registries on a schedule
		{:else}
			No detected updates. The agent compares each running image's local digest to the
			registry's remote digest on every poll.
		{/if}
	</p>
</header>

{#if error}
	<p class="error">{error}</p>
{:else if loading}
	<p class="muted">Loading…</p>
{:else if updates.length === 0}
	<p class="muted">No updates yet. Once an image tag moves ahead of what's running, it shows up here with its changelog.</p>
{:else}
	<div class="list">
		{#each updates as u (u.id)}
			<article class="card">
				<header>
					<span class="dot" style="background:{statusColor(u.status)}"></span>
					<h2><code>{u.image_ref}</code></h2>
					<span class="badge">{u.status}</span>
				</header>
				<p class="meta">
					<span class="server">{u.server_name}</span>
					<span class="sep">·</span>
					<span>{u.container_count} container{u.container_count === 1 ? '' : 's'}</span>
					<span class="sep">·</span>
					<span>seen {new Date(u.seen_at).toLocaleString()}</span>
				</p>
				<dl class="digests">
					<dt>Running</dt>
					<dd><code>{shortDigest(u.from_digest)}</code></dd>
					<dt>Available</dt>
					<dd><code>{shortDigest(u.to_digest)}</code></dd>
				</dl>
				{#if u.changelog.length > 0}
					<ul class="changelog">
						{#each u.changelog as e (e.version + e.url)}
							<li>
								<span class="version">{e.title ?? e.version}</span>
								{#if e.published_at}
									<span class="muted">{new Date(e.published_at).toLocaleDateString()}</span>
								{/if}
								{#if e.url}
									<a href={e.url} rel="external noopener noreferrer" target="_blank" class="link">release →</a>
								{/if}
							</li>
						{/each}
					</ul>
				{:else}
					<p class="muted">No changelog entries yet for this image.</p>
				{/if}
			</article>
		{/each}
	</div>
{/if}

<style>
	header h1 {
		margin: 0 0 0.3rem;
	}
	.muted {
		color: var(--fg-1);
		font-size: 0.85rem;
	}
	.error {
		color: var(--danger);
		padding: 1rem 1.25rem;
		background: var(--bg-1);
		border: 1px solid rgba(248, 113, 113, 0.3);
		border-radius: var(--radius-md);
	}
	.list {
		display: grid;
		gap: 0.9rem;
		margin-top: 1rem;
	}
	.card {
		background: var(--bg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
		padding: 1rem 1.25rem;
	}
	.card > header {
		display: flex;
		align-items: center;
		gap: 0.6rem;
	}
	.card h2 {
		margin: 0;
		font-size: 1rem;
		flex: 1;
	}
	.card h2 code {
		font-family: var(--font-mono);
		font-size: 0.92rem;
		word-break: break-all;
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--fg-1);
		flex: none;
	}
	.badge {
		font-size: 0.72rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--fg-1);
		border: 1px solid var(--border);
		border-radius: 999px;
		padding: 0.1rem 0.5rem;
	}
	.meta {
		margin: 0.4rem 0 0.6rem;
		color: var(--fg-1);
		font-size: 0.85rem;
		display: flex;
		flex-wrap: wrap;
		gap: 0.35rem;
	}
	.sep {
		opacity: 0.5;
	}
	.server {
		color: var(--fg-0);
		font-weight: 600;
	}
	.digests {
		margin: 0 0 0.8rem;
		display: grid;
		grid-template-columns: 90px 1fr;
		gap: 0.3rem 0.75rem;
		font-size: 0.88rem;
	}
	.digests dt {
		color: var(--fg-1);
	}
	.digests dd {
		margin: 0;
	}
	.digests dd code {
		font-family: var(--font-mono);
		font-size: 0.8rem;
		word-break: break-all;
	}
	.changelog {
		list-style: none;
		margin: 0;
		padding: 0.6rem 0 0;
		border-top: 1px solid var(--border);
		display: grid;
		gap: 0.4rem;
	}
	.changelog li {
		display: flex;
		align-items: baseline;
		gap: 0.6rem;
		font-size: 0.88rem;
	}
	.changelog .version {
		font-weight: 600;
	}
	.link {
		margin-left: auto;
		color: var(--fg-1);
		text-decoration: none;
		font-size: 0.78rem;
	}
	.link:hover {
		color: var(--fg-0);
	}
</style>
