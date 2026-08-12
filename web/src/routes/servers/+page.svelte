<script lang="ts">
	import { animateOn } from '$animations';

	const sections: Array<{ title: string; body: string }> = [
		{ title: 'Servers', body: 'Fleet list and per-host detail (Phase 1).' },
		{ title: 'Containers', body: 'Container grid and detail drawer (Phase 1).' },
		{ title: 'Updates', body: 'Detected updates and changelog view (Phase 2).' },
		{ title: 'Settings', body: 'User, notification, and OIDC settings (Phase 3+).' }
	];

	$effect(() => {
		animateOn('.placeholder', { opacity: [0, 1], y: [8, 0] }, { duration: 0.4, easing: 'ease-out' });
	});
</script>

<svelte:head>
	<title>Servers — DockPulse</title>
</svelte:head>

<section class="placeholder">
	<h1>Servers</h1>
	<p class="hint">
		The fleet list ships in Phase 1 once agents can enroll and report container state. This page
		exists today so the navigation tree resolves.
	</p>

	<div class="grid">
		{#each sections as section (section.title)}
			<article class="card">
				<h3>{section.title}</h3>
				<p>{section.body}</p>
			</article>
		{/each}
	</div>
</section>

<style>
	.placeholder {
		opacity: 0;
	}
	h1 {
		margin: 0 0 0.4rem;
	}
	.hint {
		color: var(--fg-1);
		max-width: 60ch;
	}
	.grid {
		margin-top: 1.5rem;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
		gap: 0.85rem;
	}
	.card {
		background: var(--bg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
		padding: 1rem;
	}
	.card h3 {
		margin: 0 0 0.3rem;
		font-size: 0.98rem;
	}
	.card p {
		margin: 0;
		color: var(--fg-1);
		font-size: 0.88rem;
	}
</style>