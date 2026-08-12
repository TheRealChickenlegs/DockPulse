<script lang="ts">
	import { onMount } from 'svelte';
	import { animateOn } from '$animations';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { api, ApiError } from '$lib/api';
	import { session } from '$lib/stores';

	let mode: 'login' | 'firstrun' = $state('login');
	let username = $state('');
	let password = $state('');
	let passwordConfirm = $state('');
	let email = $state('');
	let busy = $state(false);
	let error: string | null = $state(null);

	onMount(() => {
		let unsub = () => {};
		(async () => {
			try {
				const status = await api.firstRunStatus();
				if (status.needs_setup) {
					mode = 'firstrun';
				}
			} catch {
				/* fall through to login mode */
			}
			await session.refresh();
		})();
		unsub = session.subscribe(($s) => {
			if ($s.loaded && $s.user) {
				goto(resolve('/'));
			}
		});
		return () => unsub();
	});

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = null;
		busy = true;
		try {
			if (mode === 'firstrun') {
				if (password !== passwordConfirm) {
					error = 'Passwords do not match';
					return;
				}
				await api.firstRunCreate({ username, password, email });
			} else {
				await api.login({ username, password });
			}
			await session.refresh();
		} catch (err) {
			if (err instanceof ApiError) {
				error = err.message;
			} else {
				error = 'Network error';
			}
		} finally {
			busy = false;
		}
	}

	$effect(() => {
		animateOn('.card', { opacity: [0, 1], y: [8, 0] }, { duration: 0.4, easing: 'ease-out' });
	});
</script>

<svelte:head>
	<title>{mode === 'firstrun' ? 'Set up DockPulse' : 'Sign in'} — DockPulse</title>
</svelte:head>

<section class="wrap">
	<form class="card" onsubmit={submit}>
		<h1>{mode === 'firstrun' ? 'Welcome to DockPulse' : 'Sign in'}</h1>
		{#if mode === 'firstrun'}
			<p class="hint">
				Create the first admin account. You can add more users and enable OIDC from the settings page later.
			</p>
		{:else}
			<p class="hint">Use your DockPulse account or your configured OIDC provider.</p>
		{/if}

		<label>
			<span>Username</span>
			<input
				type="text"
				name="username"
				autocomplete="username"
				bind:value={username}
				required
				minlength="3"
				maxlength="64"
			/>
		</label>

		<label>
			<span>Password</span>
			<input
				type="password"
				name="password"
				autocomplete={mode === 'firstrun' ? 'new-password' : 'current-password'}
				bind:value={password}
				required
				minlength={mode === 'firstrun' ? 12 : 1}
			/>
		</label>

		{#if mode === 'firstrun'}
			<label>
				<span>Confirm password</span>
				<input
					type="password"
					name="password-confirm"
					autocomplete="new-password"
					bind:value={passwordConfirm}
					required
					minlength="12"
				/>
			</label>

			<label>
				<span>Email <small>(optional)</small></span>
				<input
					type="email"
					name="email"
					autocomplete="email"
					bind:value={email}
				/>
			</label>
		{/if}

		{#if error}
			<p class="error" role="alert">{error}</p>
		{/if}

		<button type="submit" disabled={busy}>
			{busy ? 'Working…' : mode === 'firstrun' ? 'Create admin' : 'Sign in'}
		</button>
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
	label small {
		color: var(--fg-1);
		opacity: 0.6;
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
</style>
