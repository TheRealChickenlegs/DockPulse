<script lang="ts">
	import { animateOn, inViewOn, staggerDelay } from '$animations';
	import { resolve } from '$app/paths';

	const features = $state([
		{
			title: 'Fleet-wide inventory',
			body: 'See every container across every registered host in a single dashboard.'
		},
		{
			title: 'Update detection',
			body: 'Periodic registry polling surfaces new image versions the moment they ship.'
		},
		{
			title: 'Changelog aggregation',
			body: 'Pulls release notes from GitHub, GitLab, and OCI image labels automatically.'
		},
		{
			title: 'Notifications',
			body: 'Discord, Slack, Ntfy, Email, and Telegram — opt in per user, per event.'
		},
		{
			title: 'Agent-based topology',
			body: 'One binary, two modes. Agents speak outbound mTLS — no inbound ports to open.'
		},
		{
			title: 'Optional one-click update',
			body: 'Apply image updates from the UI when you want to; never by accident.'
		}
	]);

	$effect(() => {
		inViewOn('.hero', (info) => {
			animateOn(info.target, { opacity: [0, 1], y: [16, 0] }, { duration: 0.6, easing: 'ease-out' });
		});
		inViewOn('.feature-grid', (info) => {
			const cards = info.target.querySelectorAll('.card');
			cards.forEach((el, i) => {
				animateOn(el, { opacity: [0, 1], y: [12, 0] }, { delay: staggerDelay(i), duration: 0.45, easing: 'ease-out' });
			});
		});
	});
</script>

<svelte:head>
	<title>DockPulse — multi-server Docker dashboard</title>
</svelte:head>

<section class="hero">
	<h1>One pulse for every container.</h1>
	<p>
		DockPulse is a homelab-friendly dashboard that pulls together inventory, update detection, and
		changelog details across all your Docker hosts.
	</p>
	<div class="cta-row">
		<a class="cta primary" href={resolve('/servers')}>Browse servers</a>
		<a class="cta" href={resolve('https://github.com/TheRealChickenlegs/DockPulse')} rel="noopener noreferrer">
			Read the docs
		</a>
	</div>
</section>

<section class="feature-grid">
	{#each features as feature (feature.title)}
		<article class="card">
			<h3>{feature.title}</h3>
			<p>{feature.body}</p>
		</article>
	{/each}
</section>

<style>
	.hero {
		padding: 4rem 0 2rem;
		opacity: 0;
	}
	.hero h1 {
		font-size: clamp(2.2rem, 5vw, 3.4rem);
		margin: 0 0 1rem;
		letter-spacing: -0.02em;
		background: var(--accent-grad);
		-webkit-background-clip: text;
		background-clip: text;
		color: transparent;
	}
	.hero p {
		font-size: 1.1rem;
		color: var(--fg-1);
		max-width: 56ch;
	}
	.cta-row {
		margin-top: 1.5rem;
		display: flex;
		gap: 0.75rem;
		flex-wrap: wrap;
	}
	.cta {
		display: inline-block;
		padding: 0.65rem 1.05rem;
		border-radius: var(--radius-md);
		border: 1px solid var(--border);
		text-decoration: none;
		color: var(--fg-0);
		background: var(--bg-1);
		transition: transform 160ms ease, background-color 160ms ease;
	}
	.cta:hover {
		transform: translateY(-1px);
		background: var(--bg-2);
	}
	.cta.primary {
		background: var(--accent-grad);
		color: #0b0f17;
		border-color: transparent;
		font-weight: 600;
	}
	.feature-grid {
		margin-top: 2.5rem;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
		gap: 1rem;
	}
	.card {
		background: var(--bg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		padding: 1.1rem 1.2rem;
		box-shadow: var(--shadow-sm);
		opacity: 0;
	}
	.card h3 {
		margin: 0 0 0.4rem;
		font-size: 1.02rem;
	}
	.card p {
		margin: 0;
		color: var(--fg-1);
		font-size: 0.93rem;
	}
</style>