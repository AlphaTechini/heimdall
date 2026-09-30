<script lang="ts">
	import { onMount } from 'svelte';
	import { ActivityFeed } from '$lib/activity.svelte';
	import { app } from '$lib/app.svelte';
	import { wallet } from '$lib/wallet.svelte';
	import EventFeedPanel from '$lib/components/EventFeedPanel.svelte';

	const feed = new ActivityFeed(50);
	let targetId = $state('');
	const uid = $props.id();

	onMount(() => feed.listen());

	$effect(() => {
		const address = wallet.address;
		const filter = targetId || null;
		void feed.load(address, filter);
	});

	const filterLabel = $derived(app.target(targetId)?.label ?? '');
</script>

<svelte:head><title>Activity - Heimdall</title></svelte:head>

<h1 class="text-3xl display sm:text-4xl">Activity</h1>
<p class="mt-2 max-w-prose text-granite-dark">
	{#if wallet.connected}
		Checks, risk changes, alerts and exits for your positions, newest first.
	{:else}
		Checks, risk changes, alerts and exits across all supported vaults, newest first. Connect your
		wallet to see only your own.
	{/if}
</p>

<div class="mt-6 max-w-sm">
	<label for="{uid}-filter" class="block text-sm font-medium">Position</label>
	<select
		id="{uid}-filter"
		class="mt-1 w-full rounded-md border-granite bg-white py-2"
		bind:value={targetId}
	>
		<option value="">All positions</option>
		{#each app.config?.targets ?? [] as t (t.id)}
			<option value={t.id}>{t.label}</option>
		{/each}
	</select>
</div>

<div class="mt-6 max-w-3xl">
	<EventFeedPanel
		{feed}
		emptyText={targetId
			? `No activity for ${filterLabel} yet. Choose All positions to see everything.`
			: 'No activity yet. Checks, alerts and exits appear here as they happen.'}
	/>
</div>
