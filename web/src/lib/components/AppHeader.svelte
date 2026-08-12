<script lang="ts">
	import { onMount } from 'svelte';
	import { session } from '$lib/stores';
	import { resolve } from '$app/paths';

	let status: 'checking' | 'ok' | 'down' = $state('checking');
	let version: string | null = $state(null);
	let menuOpen = $state(false);

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

	function toggleMenu() {
		menuOpen = !menuOpen;
	}

	async function signOut() {
		menuOpen = false;
		await session.logout();
	}
</script>

<header class="bar">
	<div class="brand">
		<a href={resolve('/')} class="brand-link">
			<span class="logo" aria-hidden="true"></span>
			<span class="name">DockPulse</span>
		</a>
	</div>
	<nav class="nav" aria-label="primary">
		<a href={resolve('/')}>Dashboard</a>
		<a href={resolve('/servers')}>Servers</a>
		<a href={resolve('/containers')}>Containers</a>
		<a href={resolve('/updates')}>Updates</a>
		<a href={resolve('/settings')}>Settings</a>
	</nav>
	<div class="status" aria-live="polite">
		<span class="dot" data-status={status}></span>
		<span class="status-label">
			{status === 'ok' ? 'Online' : status === 'down' ? 'Offline' : 'Checking…'}
		</span>
		{#if version}
			<span class="version" title="DockPulse version">v{version}</span>
		{/if}
		{#if $session.user}
			<div class="user">
				<button class="user-btn" onclick={toggleMenu} aria-haspopup="menu" aria-expanded={menuOpen}>
					{$session.user.username}
					<span class="caret" aria-hidden="true">▾</span>
				</button>
				{#if menuOpen}
					<div class="menu" role="menu">
						<div class="menu-meta">
							<div class="menu-name">{$session.user.username}</div>
							<div class="menu-role">{$session.user.role}</div>
						</div>
						<button class="menu-item" role="menuitem" onclick={signOut}>Sign out</button>
					</div>
				{/if}
			</div>
		{:else}
			<a class="sign-in" href={resolve('/login')}>Sign in</a>
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
		position: relative;
		z-index: 10;
	}
	.brand-link {
		display: flex;
		align-items: center;
		gap: 0.6rem;
		font-weight: 600;
		letter-spacing: -0.01em;
		text-decoration: none;
		color: inherit;
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
	.user {
		position: relative;
	}
	.user-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.25rem;
		padding: 0.35rem 0.55rem;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--bg-2);
		color: var(--fg-0);
		font: inherit;
		cursor: pointer;
	}
	.user-btn:hover {
		background: var(--bg-1);
	}
	.caret {
		font-size: 0.7em;
		opacity: 0.7;
	}
	.menu {
		position: absolute;
		top: calc(100% + 6px);
		right: 0;
		min-width: 200px;
		background: var(--bg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
		box-shadow: var(--shadow-md);
		padding: 0.4rem;
		display: flex;
		flex-direction: column;
		gap: 0.2rem;
	}
	.menu-meta {
		padding: 0.5rem 0.6rem;
		border-bottom: 1px solid var(--border);
		margin-bottom: 0.2rem;
	}
	.menu-name {
		font-weight: 600;
	}
	.menu-role {
		font-size: 0.75rem;
		color: var(--fg-1);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.menu-item {
		text-align: left;
		padding: 0.45rem 0.6rem;
		border-radius: var(--radius-sm);
		background: transparent;
		border: 0;
		color: var(--fg-0);
		cursor: pointer;
	}
	.menu-item:hover {
		background: var(--bg-2);
	}
	.sign-in {
		text-decoration: none;
		padding: 0.35rem 0.55rem;
		border-radius: var(--radius-sm);
		background: var(--accent-grad);
		color: #0b0f17;
		font-weight: 600;
	}
</style>
