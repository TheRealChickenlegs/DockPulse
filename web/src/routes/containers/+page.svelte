<script lang="ts">
	import { onMount } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import { page } from '$app/stores';
	import { resolve } from '$app/paths';
	import {
		api,
		type ServerListItem,
		type ContainerListItem,
		type ChangelogEntry,
		type ContainerChangelog
	} from '$lib/api';

	let servers: ServerListItem[] = $state([]);
	let selectedId: string = $state('');
	let containers: ContainerListItem[] = $state([]);
	let loadingServers = $state(true);
	let loadingContainers = $state(false);
	let error: string | null = $state(null);
	let selected: ContainerListItem | null = $state(null);
	let changelog: ContainerChangelog | null = $state(null);
	let loadingChangelog = $state(false);
	let changelogError: string | null = $state(null);
	let updateNote: string | null = $state(null);
	let scanning = $state(false);
	let collapsed = new SvelteSet<string>();

	// Containers grouped by their Docker Compose stack
	// (com.docker.compose.project). Containers outside any stack share
	// a trailing "Other" group so nothing is lost.
	type Group = { key: string; label: string; containers: ContainerListItem[] };
	const groups = $derived(groupContainers(containers));

	function groupContainers(list: ContainerListItem[]): Group[] {
		const byStack: Record<string, ContainerListItem[]> = {};
		for (const c of list) {
			const key = c.stack?.trim() || '';
			const arr = byStack[key];
			if (arr) arr.push(c);
			else byStack[key] = [c];
		}
		const out = Object.entries(byStack)
			.filter(([key]) => key !== '')
			.sort((a, b) => a[0].localeCompare(b[0]))
			.map(([key, cs]) => ({ key, label: key, containers: cs }));
		const others = byStack[''];
		if (others?.length) out.push({ key: '', label: 'Other', containers: others });
		return out;
	}

	function groupRunning(g: Group) {
		return g.containers.filter((c) => c.state === 'running').length;
	}

	function toggleGroup(key: string) {
		if (collapsed.has(key)) collapsed.delete(key);
		else collapsed.add(key);
	}

	function normVersion(s: string) {
		return s.replace(/^v/i, '').toLowerCase();
	}

	function shortDigest(d: string) {
		if (!d) return '';
		return d.length > 19 ? d.slice(0, 19) + '…' : d;
	}

	// currentIndex locates the release entry matching the running tag.
	function currentIndex() {
		if (!changelog) return -1;
		const want = normVersion(changelog.current_version);
		return changelog.entries.findIndex((e) => normVersion(e.version) === want);
	}

	function onUpdate() {
		updateNote = 'Applying updates is a planned opt-in feature and not wired up yet.';
	}

	async function loadServers() {
		loadingServers = true;
		error = null;
		try {
			const res = await api.listServers();
			servers = res.servers;
			if (servers.length > 0 && !selectedId) {
				const preset = $page.url.searchParams.get('server');
				const match = preset ? servers.find((s) => s.id === preset) : undefined;
				selectedId = (match ?? servers[0]).id;
				await loadContainers(selectedId);
			}
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to load servers';
		} finally {
			loadingServers = false;
		}
	}

	// Follow links from the servers page: if the URL gains a
	// ?server= param (e.g. via SvelteKit client-side navigation),
	// select that server without waiting for the next mount.
	$effect(() => {
		const preset = $page.url.searchParams.get('server');
		if (preset && preset !== selectedId && servers.some((s) => s.id === preset)) {
			selectedId = preset;
		}
	});

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

	async function scanNow() {
		if (!selectedId || scanning) return;
		scanning = true;
		try {
			await api.refreshServer(selectedId);
			// The agent picks the command up within ~10s; reload a few
			// times so the refreshed snapshot shows up without a manual
			// refresh.
			setTimeout(() => loadContainers(selectedId), 3000);
			setTimeout(() => loadServers(), 13_000);
		} catch (err) {
			error = err instanceof Error ? err.message : 'Failed to request scan';
		} finally {
			scanning = false;
		}
	}

	$effect(() => {
		if (selectedId) loadContainers(selectedId);
	});

	function open(c: ContainerListItem) {
		selected = c;
		changelog = null;
		changelogError = null;
		updateNote = null;
		loadingChangelog = true;
		api
			.listContainerChangelog(c.id)
			.then((res) => {
				changelog = res;
			})
			.catch((err) => {
				changelogError = err instanceof Error ? err.message : 'Failed to load changelog';
			})
			.finally(() => {
				loadingChangelog = false;
			});
	}
	function close() {
		selected = null;
		changelog = null;
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
		<header class="pane-head">
			<div>
				<h1>{servers.find((s) => s.id === selectedId)?.name ?? 'Containers'}</h1>
				<p class="muted">
					{#if selectedId}
						{containers.length} container{containers.length === 1 ? '' : 's'} · snapshots refresh every 2 minutes
					{:else}
						Pick a server on the left to see its containers.
					{/if}
				</p>
			</div>
			{#if selectedId}
				<button type="button" class="scan" onclick={scanNow} disabled={scanning} title="Trigger a new scan now">
					{scanning ? 'Scanning…' : 'Scan now'}
				</button>
			{/if}
		</header>

		{#if error}
			<p class="error">{error}</p>
		{:else if loadingContainers}
			<p class="muted">Loading…</p>
		{:else if containers.length === 0}
			<p class="muted">No containers cached for this server yet. Hit “Scan now” to fetch them immediately.</p>
		{:else}
			{#each groups as group (group.key)}
				<section class="group" class:collapsed={collapsed.has(group.key)}>
					<button
						type="button"
						class="group-head"
						aria-expanded={!collapsed.has(group.key)}
						onclick={() => toggleGroup(group.key)}
					>
						<span class="chevron" aria-hidden="true"></span>
						<span class="group-name">{group.label}</span>
						<span class="group-count">{groupRunning(group)}/{group.containers.length}</span>
					</button>
					{#if !collapsed.has(group.key)}
						<div class="grid">
							{#each group.containers as c (c.id)}
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
			{/each}
		{/if}
	</section>
</section>

{#snippet entry(e: ChangelogEntry)}
	<li class="entry">
		<div class="entry-head">
			<span class="version">{e.title ?? e.version}</span>
			{#if e.published_at}
				<span class="muted">{new Date(e.published_at).toLocaleDateString()}</span>
			{/if}
			{#if e.url}
				<a href={e.url} rel="external noopener noreferrer" target="_blank" class="link">release →</a>
			{/if}
		</div>
		{#if e.body}
			<div class="notes">{e.body}</div>
		{/if}
	</li>
{/snippet}

{#if selected}
	<div class="modal-backdrop" onclick={close} role="presentation"></div>
	<div class="modal" role="dialog" aria-modal="true" aria-label={selected.name}>
		<header class="modal-head">
			<div class="modal-title">
				<h2>{selected.name}</h2>
				<p class="muted"><code>{selected.image_ref}</code></p>
			</div>
			<button class="close" type="button" onclick={close} aria-label="Close">×</button>
		</header>

		<dl class="details">
			<dt>State</dt>
			<dd>
				<span class="dot" style="background:{stateColor(selected.state)}"></span>
				{selected.state}
			</dd>
			<dt>Local digest</dt>
			<dd><code>{selected.image_digest_local || '—'}</code></dd>
			<dt>Started</dt>
			<dd>{selected.started_at ? new Date(selected.started_at).toLocaleString() : '—'}</dd>
			<dt>Docker ID</dt>
			<dd><code>{selected.docker_id}</code></dd>
			<dt>Updated</dt>
			<dd>{new Date(selected.updated_at).toLocaleString()}</dd>
		</dl>

		{#if loadingChangelog}
			<p class="muted">Loading…</p>
		{:else if changelogError}
			<p class="muted changelog-error">{changelogError}</p>
		{:else if changelog}
			{#if changelog.update?.available}
				<section class="update">
					<header class="section-head">
						<h3>Update available</h3>
						{#if changelog.update.new_version}
							<span class="new-version">{changelog.update.new_version}</span>
						{/if}
					</header>
					{#if changelog.update.to_digest}
						<p class="muted"><code>{shortDigest(changelog.update.to_digest)}</code></p>
					{/if}
					{#if currentIndex() > 0}
						<ul class="changelog">
							{#each changelog.entries.slice(0, currentIndex()) as e (e.version + (e.url ?? ''))}
								{@render entry(e)}
							{/each}
						</ul>
					{/if}
					<button type="button" class="update-btn" onclick={onUpdate}>Update</button>
					{#if updateNote}
						<p class="muted update-note">{updateNote}</p>
					{/if}
				</section>
			{/if}

			<section class="current">
				<h3 class="section-title">Current version</h3>
				<p class="current-version">
					<span class="tag current">running</span>
					{changelog.current_version}
				</p>
				{#if currentIndex() >= 0}
					<ul class="changelog">
						{@render entry(changelog.entries[currentIndex()])}
					</ul>
				{:else}
					<p class="muted">No release notes for this version.</p>
				{/if}
			</section>

			{#if changelog.entries.length > 0}
				<section class="history">
					<h3 class="section-title">Release history</h3>
					<ul class="changelog">
						{#each changelog.entries as e, i (e.version + (e.url ?? ''))}
							{#if i > currentIndex()}
								{@render entry(e)}
							{/if}
						{/each}
					</ul>
				</section>
			{/if}
		{/if}
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
	.pane-head {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 1rem;
		margin-bottom: 1rem;
	}
	.pane h1 {
		margin: 0 0 0.2rem;
	}
	.scan {
		font: inherit;
		font-size: 0.8rem;
		font-weight: 600;
		padding: 0.35rem 0.8rem;
		background: transparent;
		color: var(--fg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		cursor: pointer;
		white-space: nowrap;
	}
	.scan:hover:not(:disabled) {
		color: var(--fg-0);
		border-color: rgba(129, 140, 248, 0.5);
	}
	.scan:disabled {
		opacity: 0.6;
		cursor: default;
	}
	.group {
		margin-bottom: 1.2rem;
	}
	.group-head {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		width: 100%;
		padding: 0.5rem 0.7rem;
		margin-bottom: 0.8rem;
		background: var(--bg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
		color: inherit;
		font: inherit;
		cursor: pointer;
		text-align: left;
	}
	.group-head:hover {
		border-color: rgba(129, 140, 248, 0.5);
	}
	.chevron {
		width: 8px;
		height: 8px;
		border-right: 2px solid var(--fg-1);
		border-bottom: 2px solid var(--fg-1);
		transform: rotate(-45deg);
		transition: transform 160ms ease;
		flex-shrink: 0;
	}
	.group:not(.collapsed) .chevron {
		transform: rotate(45deg);
	}
	.group-name {
		font-weight: 600;
	}
	.group-count {
		margin-left: auto;
		font-family: var(--font-mono);
		font-size: 0.78rem;
		color: var(--fg-1);
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

	.modal-backdrop {
		position: fixed;
		inset: 0;
		background: rgba(0, 0, 0, 0.5);
		z-index: 30;
		animation: fade 0.2s ease;
	}
	.modal {
		position: fixed;
		top: 50%;
		left: 50%;
		transform: translate(-50%, -50%);
		width: min(720px, calc(100vw - 2rem));
		max-height: min(85vh, 900px);
		background: var(--bg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
		padding: 1.25rem 1.5rem;
		overflow-y: auto;
		z-index: 31;
		animation: modalIn 0.2s cubic-bezier(0.2, 0.8, 0.2, 1);
		box-shadow: var(--shadow-md);
	}
	.modal-head {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 1rem;
		margin-bottom: 1rem;
	}
	.modal-title h2 {
		margin: 0 0 0.2rem;
	}
	.modal-title p {
		margin: 0;
	}
	.modal-title code {
		font-family: var(--font-mono);
		font-size: 0.82rem;
		word-break: break-all;
	}
	.close {
		background: transparent;
		border: 0;
		color: var(--fg-1);
		font-size: 1.5rem;
		line-height: 1;
		cursor: pointer;
		flex: none;
	}
	.close:hover {
		color: var(--fg-0);
	}
	.details {
		margin: 0 0 1rem;
		display: grid;
		grid-template-columns: 130px 1fr;
		gap: 0.5rem 0.75rem;
		font-size: 0.9rem;
	}
	.details dt {
		color: var(--fg-1);
	}
	.details dd {
		margin: 0;
	}
	.details dd code {
		font-family: var(--font-mono);
		font-size: 0.83rem;
		word-break: break-all;
	}
	.section-title {
		margin: 0 0 0.5rem;
		font-size: 0.8rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--fg-1);
	}
	.update {
		margin: 0 0 1.25rem;
		padding: 0.9rem 1rem;
		border: 1px solid rgba(250, 204, 21, 0.4);
		border-radius: var(--radius-md);
		background: rgba(250, 204, 21, 0.06);
	}
	.section-head {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		margin-bottom: 0.5rem;
	}
	.section-head h3 {
		margin: 0;
		font-size: 0.95rem;
	}
	.new-version {
		font-family: var(--font-mono);
		font-size: 0.82rem;
		font-weight: 600;
		padding: 0.05rem 0.5rem;
		border-radius: 999px;
		border: 1px solid var(--border);
	}
	.update-btn {
		margin-top: 0.75rem;
		font: inherit;
		font-size: 0.85rem;
		font-weight: 600;
		padding: 0.4rem 1rem;
		background: var(--accent-grad);
		color: #0b0f17;
		border: 0;
		border-radius: var(--radius-sm);
		cursor: pointer;
	}
	.update-btn:hover {
		filter: brightness(1.08);
	}
	.update-note {
		margin: 0.5rem 0 0;
		font-size: 0.8rem;
	}
	.current {
		margin: 0 0 1.25rem;
	}
	.current-version {
		margin: 0 0 0.5rem;
		display: flex;
		align-items: center;
		gap: 0.6rem;
		font-family: var(--font-mono);
		font-weight: 600;
	}
	.history {
		border-top: 1px solid var(--border);
		padding-top: 1rem;
	}
	.changelog {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 0.9rem;
		font-size: 0.88rem;
	}
	.entry {
		padding-bottom: 0.9rem;
		border-bottom: 1px solid var(--border);
	}
	.entry:last-child {
		border-bottom: 0;
		padding-bottom: 0;
	}
	.entry-head {
		display: flex;
		align-items: baseline;
		gap: 0.6rem;
		flex-wrap: wrap;
	}
	.entry .version {
		font-weight: 600;
	}
	.notes {
		margin-top: 0.4rem;
		white-space: pre-wrap;
		word-break: break-word;
		color: var(--fg-0);
		font-size: 0.85rem;
		line-height: 1.5;
	}
	.tag {
		font-size: 0.66rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		padding: 0.05rem 0.45rem;
		border-radius: 999px;
		white-space: nowrap;
	}
	.tag.current {
		color: var(--ok);
		border: 1px solid rgba(74, 222, 128, 0.4);
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
	.changelog-error {
		color: var(--danger);
	}
	@keyframes fade {
		from {
			opacity: 0;
		}
		to {
			opacity: 1;
		}
	}
	@keyframes modalIn {
		from {
			transform: translate(-50%, -48%);
			opacity: 0;
		}
		to {
			transform: translate(-50%, -50%);
			opacity: 1;
		}
	}
</style>
