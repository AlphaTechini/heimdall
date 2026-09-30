<script lang="ts">
	import { txLink } from '$lib/explorer';
	import { formatClock, formatDateTime } from '$lib/format';
	import type { EventKind, HeimdallEvent } from '$lib/types';
	import ExplorerLink from './ExplorerLink.svelte';

	let { events, compact = false }: { events: HeimdallEvent[]; compact?: boolean } = $props();

	const KIND: Record<EventKind, string> = {
		check: 'Check',
		severity: 'Risk level changed',
		alert: 'Alert',
		exit_submitted: 'Exit submitted',
		exit_partial: 'Partial exit',
		exit_complete: 'Exit complete',
		exit_deferred: 'Exit waiting for cash',
		exit_failed: 'Exit failed',
		guard: 'Guard',
		sim: 'Simulation'
	};

	// Marker color only where it means severity or the outcome of an exit.
	function marker(e: HeimdallEvent): string {
		if (e.kind === 'exit_complete') return 'border-fjord';
		if (e.severity === 'critical' || e.kind === 'exit_failed') return 'border-ember';
		if (e.severity === 'warning') return 'border-amber';
		return 'border-transparent';
	}
</script>

<ol class="divide-y divide-line" aria-live="polite" aria-relevant="additions">
	{#each events as event (event.id)}
		<li class="border-l-4 py-3 pl-4 {marker(event)}">
			<div class="flex flex-wrap items-baseline gap-x-3 gap-y-0.5">
				<span class="font-semibold">{KIND[event.kind] ?? event.kind}</span>
				<time
					class="text-sm text-granite-dark tabular"
					datetime={event.time}
					title={formatDateTime(event.time)}>{formatClock(event.time)}</time
				>
			</div>
			<p class="mt-0.5 {compact ? 'text-sm' : ''}">{event.message}</p>
			<p class="mt-0.5 flex flex-wrap gap-x-4 text-sm text-granite-dark tabular">
				<span>Block {event.block}</span>
				{#if event.txHash}
					<ExplorerLink link={txLink(event.txHash)}>View transaction</ExplorerLink>
				{/if}
			</p>
		</li>
	{/each}
</ol>
