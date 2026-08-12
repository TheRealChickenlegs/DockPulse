<script lang="ts">
	import { animateOn } from '$animations';

	$effect(() => {
		animateOn('.placeholder', { opacity: [0, 1], y: [8, 0] }, { duration: 0.4, easing: 'ease-out' });
	});
</script>

<svelte:head>
	<title>Settings — DockPulse</title>
</svelte:head>

<section class="placeholder">
	<h1>Settings</h1>
	<p>User, notification, and OIDC settings ship in Phase 3+.</p>
</section>

<style>
	.placeholder {
		opacity: 0;
	}
	h1 {
		margin: 0 0 0.4rem;
	}
</style>