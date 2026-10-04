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

<h1 class="title">Activity</h1>
<p class="mt-3 max-w-[58ch] text-[1.0625rem] text-granite-dark">
	{#if wallet.connected}
		Checks, risk changes, alerts and exits for your positions, newest first.
	{:else}
		Checks, risk changes, alerts and exits across all supported vaults, newest first. Connect your
		wallet to see only your own.
	{/if}
</p>

<div class="mt-8 max-w-sm">
	<label for="{uid}-filter" class="block text-sm font-medium">Position</label>
	<select
		id="{uid}-filter"
		class="mt-1.5 w-full rounded-[10px] border-[#c5ced4] bg-white py-2.5"
		bind:value={targetId}
	>
		<option value="">All positions</option>
		{#each app.config?.targets ?? [] as t (t.id)}
			<option value={t.id}>{t.label}</option>
		{/each}
	</select>
</div>

<div class="mt-6 max-w-3xl panel px-5 py-5">
	<EventFeedPanel
		{feed}
		emptyText={targetId
			? `No activity for ${filterLabel} yet. Choose All positions to see everything.`
			: 'No activity yet. Checks, alerts and exits appear here as they happen.'}
	/>
</div>
