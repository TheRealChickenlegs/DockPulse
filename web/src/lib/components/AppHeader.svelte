<script lang="ts">
	import { onMount } from 'svelte';

	let status: 'checking' | 'ok' | 'down' = $state('checking');
	let version: string | null = $state(null);

	onMount(async () => {
		try {
			const res = await fetch('/healthz', { credentials: 'include' });
			if (res.ok) {
				const data = await res.json();
				status = data.status === 'ok' ? 'ok' : 'down';
			} else {
				status = 'down';
			}
		} catch {
			status = 'down';
		}
		try {
			const v = await fetch('/version');
			if (v.ok) {
				const j = await v.json();
				version = j.version ?? null;
			}
		} catch {
			/* version is best-effort */
		}
	});
</script>

<header class="bar">
	<div class="brand">
		<span class="logo" aria-hidden="true"></span>
		<span class="name">DockPulse</span>
	</div>
	<nav class="nav" aria-label="primary">
		<a href="/" aria-current="page">Dashboard</a>
		<a href="/servers">Servers</a>
		<a href="/containers">Containers</a>
		<a href="/updates">Updates</a>
		<a href="/settings">Settings</a>
	</nav>
	<div class="status" aria-live="polite">
		<span class="dot" data-status={status}></span>
		<span class="status-label">
			{status === 'ok' ? 'Online' : status === 'down' ? 'Offline' : 'Checking…'}
		</span>
		{#if version}
			<span class="version" title="DockPulse version">v{version}</span>
		{/if}
	</div>
</header>

<style>
	.bar {
		display: flex;
		align-items: center;
		gap: 1.5rem;
		padding: 0.75rem 1.25rem;
		border-bottom: 1px solid var(--border);
		background: var(--bg-1);
		backdrop-filter: saturate(140%);
	}
	.brand {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	.logo {
		width: 22px;
		height: 22px;
		border-radius: 6px;
		background: var(--accent-grad);
		box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.06) inset, 0 4px 10px rgba(94, 234, 212, 0.25);
		display: inline-block;
	}
	.nav {
		display: flex;
		gap: 1.1rem;
		margin-left: 1rem;
		font-size: 0.92rem;
		color: var(--fg-1);
	}
	.nav a {
		text-decoration: none;
		padding: 0.35rem 0.55rem;
		border-radius: var(--radius-sm);
		transition: background-color 180ms ease, color 180ms ease;
	}
	.nav a:hover {
		color: var(--fg-0);
		background: var(--bg-2);
	}
	.nav a[aria-current='page'] {
		color: var(--fg-0);
		background: var(--bg-2);
	}
	.status {
		margin-left: auto;
		display: flex;
		align-items: center;
		gap: 0.55rem;
		color: var(--fg-1);
		font-size: 0.85rem;
	}
	.dot {
		width: 9px;
		height: 9px;
		border-radius: 50%;
		background: var(--fg-1);
		display: inline-block;
	}
	.dot[data-status='ok'] {
		background: var(--ok);
		box-shadow: 0 0 0 4px rgba(52, 211, 153, 0.15);
	}
	.dot[data-status='down'] {
		background: var(--danger);
	}
	.dot[data-status='checking'] {
		background: var(--warn);
	}
	.version {
		font-family: var(--font-mono);
		color: var(--fg-1);
		opacity: 0.7;
	}
</style>