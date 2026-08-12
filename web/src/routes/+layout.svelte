<script lang="ts">
	import '../app.css';
	import AppHeader from '$components/AppHeader.svelte';

	let { children } = $props();
</script>

<AppHeader />

<main class="content">
	{@render children?.()}
</main>

<style>
	.content {
		max-width: 1200px;
		margin: 0 auto;
		padding: 1.75rem 1.25rem 4rem;
	}
</style>