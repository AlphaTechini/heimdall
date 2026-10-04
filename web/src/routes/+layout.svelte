<script lang="ts">
	import './layout.css';
	import { onMount, untrack } from 'svelte';
	import { app } from '$lib/app.svelte';
	import { startClock } from '$lib/clock.svelte';
	import { live } from '$lib/live.svelte';
	import { positions, watchPositions } from '$lib/positions.svelte';
	import { wallet } from '$lib/wallet.svelte';
	import ErrorState from '$lib/components/ErrorState.svelte';
	import Header from '$lib/components/Header.svelte';
	import Toasts from '$lib/components/Toasts.svelte';

	let { children } = $props();

	onMount(() => {
		startClock();
		wallet.start();
		void app.load();
		live.start();
		return watchPositions();
	});

	// Follow the connected wallet: load its positions, and again once the wallet is on the right
	// network so the Guard's switches can be read from the chain.
	$effect(() => {
		const address = wallet.address;
		untrack(() => void positions.setAddress(address));
	});
	$effect(() => {
		if (wallet.address && !wallet.issue) untrack(() => void positions.load());
	});
</script>

<svelte:head>
	<title>Heimdall</title>
</svelte:head>

<a
	href="#main"
	class="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50 focus:rounded-[10px] focus:border focus:border-line focus:bg-white focus:px-4 focus:py-2.5 focus:font-medium focus:text-ink"
>
	Skip to content
</a>

<Header />

<main id="main" class="mx-auto max-w-6xl px-4 py-8 sm:px-6 sm:py-10">
	{#if app.status === 'ready' && !wallet.restoring}
		{@render children()}
	{:else if app.status === 'error'}
		<ErrorState title="Heimdall cannot be reached" message={app.error} onretry={() => app.load()} />
	{:else}
		<div class="max-w-2xl space-y-4" aria-hidden="true">
			<div class="h-10 w-2/3 skeleton"></div>
			<div class="h-48 w-full skeleton"></div>
		</div>
		<p class="sr-only" role="status">Loading Heimdall</p>
	{/if}
</main>

<Toasts />
