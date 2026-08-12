<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import AppHeader from '$components/AppHeader.svelte';
	import { session } from '$lib/stores';

	let { children } = $props();

	onMount(() => {
		session.refresh();
	});
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
