<script lang="ts">
	import { animateOn } from '$animations';

	let username = $state('');
	let password = $state('');
	let busy = $state(false);
	let error: string | null = $state(null);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = null;
		busy = true;
		try {
			// Placeholder until the auth endpoint lands in Phase 1.
			await new Promise((r) => setTimeout(r, 400));
			error = 'Login is not implemented yet (Phase 1).';
		} finally {
			busy = false;
		}
	}

	$effect(() => {
		animateOn('.card', { opacity: [0, 1], y: [8, 0] }, { duration: 0.4, easing: 'ease-out' });
	});
</script>

<svelte:head>
	<title>Sign in — DockPulse</title>
</svelte:head>

<section class="wrap">
	<form class="card" onsubmit={submit}>
		<h1>Sign in</h1>
		<p class="hint">Use your DockPulse account or your configured OIDC provider.</p>

		<label>
			<span>Username</span>
			<input type="text" name="username" autocomplete="username" bind:value={username} required />
		</label>

		<label>
			<span>Password</span>
			<input
				type="password"
				name="password"
				autocomplete="current-password"
				bind:value={password}
				required
			/>
		</label>

		{#if error}
			<p class="error" role="alert">{error}</p>
		{/if}

		<button type="submit" disabled={busy}>{busy ? 'Signing in…' : 'Sign in'}</button>

		<p class="meta">Phase 0 placeholder. The auth endpoint is added in Phase 1.</p>
	</form>
</section>

<style>
	.wrap {
		min-height: 70vh;
		display: grid;
		place-items: center;
		padding: 2rem 1rem;
	}
	.card {
		width: 100%;
		max-width: 380px;
		background: var(--bg-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-lg);
		padding: 1.5rem;
		box-shadow: var(--shadow-md);
		opacity: 0;
	}
	h1 {
		margin: 0 0 0.35rem;
		font-size: 1.35rem;
	}
	.hint {
		color: var(--fg-1);
		margin: 0 0 1rem;
		font-size: 0.9rem;
	}
	label {
		display: block;
		margin-bottom: 0.85rem;
	}
	label span {
		display: block;
		font-size: 0.85rem;
		color: var(--fg-1);
		margin-bottom: 0.3rem;
	}
	input {
		width: 100%;
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
	button {
		width: 100%;
		padding: 0.6rem;
		border-radius: var(--radius-sm);
		background: var(--accent-grad);
		color: #0b0f17;
		font-weight: 600;
		border: 0;
		cursor: pointer;
	}
	button:disabled {
		opacity: 0.6;
		cursor: progress;
	}
	.error {
		color: var(--danger);
		font-size: 0.88rem;
		margin: 0.6rem 0;
	}
	.meta {
		color: var(--fg-1);
		font-size: 0.78rem;
		text-align: center;
		margin: 1rem 0 0;
		opacity: 0.7;
	}
</style>