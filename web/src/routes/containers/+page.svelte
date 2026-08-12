<script lang="ts">
	import { animateOn } from '$animations';

	$effect(() => {
		animateOn('.placeholder', { opacity: [0, 1], y: [8, 0] }, { duration: 0.4, easing: 'ease-out' });
	});
</script>

<svelte:head>
	<title>Containers — DockPulse</title>
</svelte:head>

<section class="placeholder">
	<h1>Containers</h1>
	<p>Container grid and detail drawer ship in Phase 1.</p>
</section>

<style>
	.placeholder {
		opacity: 0;
	}
	h1 {
		margin: 0 0 0.4rem;
	}
</style>