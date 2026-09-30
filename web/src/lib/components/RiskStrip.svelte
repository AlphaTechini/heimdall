<script lang="ts">
	import { app } from '$lib/app.svelte';
	import { clock } from '$lib/clock.svelte';
	import { formatReading, timeAgo } from '$lib/format';
	import { live } from '$lib/live.svelte';
	import type { SignalLevel } from '$lib/types';

	let { targetId }: { targetId: string } = $props();

	const snapshot = $derived(live.snapshots[targetId]);
	const error = $derived(live.snapshotErrors[targetId]);
	const infos = $derived(app.config?.signals ?? []);
	let expanded = $state<string | null>(null);
	let retrying = $state(false);
	const uid = $props.id();

	const LEVEL_TEXT: Record<SignalLevel, string> = {
		ok: 'Normal',
		warning: 'Warning',
		critical: 'Critical',
		unavailable: 'Not available'
	};
	// Severity colors appear here because this is the risk strip; the text word always says the level too.
	const LEVEL_BAR: Record<SignalLevel, string> = {
		ok: 'bg-calm',
		warning: 'bg-amber',
		critical: 'bg-ember',
		unavailable: 'bg-granite'
	};

	const rows = $derived(
		infos.map((info) => ({
			info,
			reading: snapshot?.signals.find((s) => s.id === info.id)
		}))
	);
	const open = $derived(rows.find((r) => r.info.id === expanded));

	async function retry() {
		retrying = true;
		await live.loadSnapshot(targetId);
		retrying = false;
	}
</script>

<div>
	{#if snapshot}
		<ul class="grid grid-cols-2 gap-2 sm:grid-cols-3" aria-label="Risk signals">
			{#each rows as { info, reading } (info.id)}
				{@const level = reading?.level ?? 'unavailable'}
				<li>
					<button
						type="button"
						class="block h-full w-full rounded-md border border-line bg-frost text-left hover:border-granite"
						aria-expanded={expanded === info.id}
						aria-controls="{uid}-detail"
						title={info.tooltip}
						onclick={() => (expanded = expanded === info.id ? null : info.id)}
					>
						<span class="block h-1 rounded-t-md {LEVEL_BAR[level]}"></span>
						<span class="block px-3 py-2">
							<span class="block text-sm font-medium">{info.label}</span>
							<span class="block text-sm text-granite-dark tabular">
								{#if reading && level !== 'unavailable'}
									{formatReading(reading.value)} {reading.unit}
								{:else}
									No reading
								{/if}
							</span>
							<span class="block text-sm font-semibold">{LEVEL_TEXT[level]}</span>
						</span>
					</button>
				</li>
			{/each}
		</ul>
		<div id="{uid}-detail" class="mt-2 min-h-[2.75rem] text-sm">
			{#if open}
				<p>
					<span class="font-semibold">{open.info.label}.</span>
					{open.info.tooltip}
					<span class="text-granite-dark"
						>{open.reading?.detail ?? 'No reading yet for this signal.'}</span
					>
				</p>
			{:else}
				<p class="text-granite-dark">
					Select a signal to see what it means and its latest reading.
				</p>
			{/if}
		</div>
		<p class="text-sm text-granite-dark tabular" aria-live="off">
			{#if live.connected}
				Watching {rows.length} signals. Last check {timeAgo(snapshot.updatedAt, clock.now)}, block {snapshot.block}.
			{:else}
				Live updates paused, reconnecting. Last check {timeAgo(snapshot.updatedAt, clock.now)}.
			{/if}
		</p>
	{:else if error}
		<div
			class="flex flex-wrap items-center gap-3 rounded-md border border-line bg-frost px-3 py-3 text-sm"
		>
			<p class="min-w-0 flex-1 basis-56">{error}</p>
			<button
				type="button"
				class="rounded-md border border-fjord px-3 py-1.5 font-medium text-fjord hover:bg-white disabled:opacity-60"
				disabled={retrying}
				onclick={retry}
			>
				{retrying ? 'Trying again' : 'Try again'}
			</button>
		</div>
	{:else}
		<div class="grid grid-cols-2 gap-2 sm:grid-cols-3" aria-hidden="true">
			{#each [...Array(infos.length || 6).keys()] as i (i)}
				<div class="h-[5.25rem] skeleton"></div>
			{/each}
		</div>
		<p class="sr-only" role="status">Loading risk signals</p>
	{/if}
</div>
