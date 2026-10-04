<script lang="ts">
	import type { ActivityFeed } from '$lib/activity.svelte';
	import ErrorState from './ErrorState.svelte';
	import EventList from './EventList.svelte';

	let {
		feed,
		compact = false,
		emptyText
	}: { feed: ActivityFeed; compact?: boolean; emptyText: string } = $props();
</script>

{#if feed.status === 'loading' && feed.events.length === 0}
	<div class="space-y-4" aria-hidden="true">
		{#each [0, 1, 2, 3] as i (i)}
			<div class="space-y-2">
				<div class="h-4 w-2/5 skeleton"></div>
				<div class="h-4 w-full skeleton"></div>
				<div class="h-3 w-1/4 skeleton"></div>
			</div>
		{/each}
	</div>
	<p class="sr-only" role="status">Loading activity</p>
{:else if feed.status === 'error'}
	<ErrorState title="Activity did not load" message={feed.error} onretry={() => feed.retry()} />
{:else if feed.events.length === 0}
	<p class="text-granite-dark">{emptyText}</p>
{:else}
	<EventList events={feed.events} {compact} />
{/if}
