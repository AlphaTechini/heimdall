<script lang="ts">
	import { resolve } from '$app/paths';
	import { api } from '$lib/api';
	import { app } from '$lib/app.svelte';
	import { describeError } from '$lib/errors';
	import { formatDateTime, formatNumber } from '$lib/format';
	import type { Backtest } from '$lib/types';
	import BacktestChart from '$lib/components/BacktestChart.svelte';
	import ErrorState from '$lib/components/ErrorState.svelte';

	const uid = $props.id();
	let selected = $state('');
	let data = $state<Backtest | null>(null);
	let status = $state<'idle' | 'loading' | 'ready' | 'error'>('idle');
	let error = $state('');

	// Pick the first incident once the list is known.
	$effect(() => {
		if (!selected && app.backtests.length > 0) selected = app.backtests[0].id;
	});

	async function load(id: string, signal?: AbortSignal) {
		status = 'loading';
		try {
			const res = await api.backtest(id, signal);
			if (signal?.aborted) return;
			data = res;
			status = 'ready';
		} catch (e) {
			if (signal?.aborted) return;
			error = describeError(e, 'Could not load this incident.').message;
			status = 'error';
		}
	}

	$effect(() => {
		const id = selected;
		if (!id) return;
		const controller = new AbortController();
		void load(id, controller.signal);
		return () => controller.abort();
	});

	/** The server's `note` with its source URLs turned into links (where the addresses and block range came from). */
	const noteParts = $derived.by(() => {
		if (!data) return [];
		const parts: { text: string; url: boolean }[] = [];
		let rest = data.note;
		for (const m of data.note.matchAll(/https?:\/\/[^\s,;)]+/g)) {
			const at = rest.indexOf(m[0]);
			if (at > 0) parts.push({ text: rest.slice(0, at), url: false });
			parts.push({ text: m[0], url: true });
			rest = rest.slice(at + m[0].length);
		}
		if (rest) parts.push({ text: rest, url: false });
		return parts;
	});

	const summary = $derived.by(() => {
		if (!data) return '';
		if (data.firstCriticalBlock === null) {
			return "Heimdall's rules never reached Critical over this range, so no exit would have started.";
		}
		const pct = data.pctOfPeakRemainingAtFirstCritical;
		const remaining =
			pct === null
				? ''
				: ` About ${formatNumber(pct, 1)}% of the peak balance was still in the pool at that moment.`;
		return `Heimdall's rules reach Critical at block ${data.firstCriticalBlock}.${remaining}`;
	});
</script>

<svelte:head><title>Backtest - Heimdall</title></svelte:head>

<h1 class="title">Replay a real hack</h1>

{#if !app.backtestsLoaded}
	<div class="mt-6 h-10 w-72 skeleton" aria-hidden="true"></div>
	<p class="sr-only" role="status">Loading incidents</p>
{:else if app.backtests.length === 0}
	<p class="mt-3 max-w-[58ch] text-[1.0625rem] text-granite-dark">
		No incidents with real on-chain data are loaded on this server, so there is nothing to replay.
		Go back to the <a href={resolve('/')}>dashboard</a>.
	</p>
{:else}
	<div class="mt-8 max-w-md">
		<label for="{uid}-incident" class="block text-sm font-medium">Incident</label>
		<select
			id="{uid}-incident"
			class="mt-1.5 w-full rounded-[10px] border-[#c5ced4] bg-white py-2.5"
			bind:value={selected}
		>
			{#each app.backtests as b (b.id)}
				<option value={b.id}>{b.title}</option>
			{/each}
		</select>
	</div>

	<div class="mt-8">
		{#if status === 'loading' || status === 'idle'}
			<div class="max-w-3xl space-y-3" aria-hidden="true">
				<div class="h-72 w-full skeleton"></div>
				<div class="h-5 w-2/3 skeleton"></div>
			</div>
			<p class="sr-only" role="status">Loading incident</p>
		{:else if status === 'error'}
			<ErrorState
				title="This incident did not load"
				message={error}
				onretry={() => load(selected)}
			/>
		{:else if data}
			{#if data.points.length === 0}
				<p class="max-w-prose text-granite-dark">This incident has no recorded points.</p>
			{:else}
				<div class="max-w-4xl panel px-5 py-5">
					<BacktestChart {data} />
				</div>
				<p class="mt-5 max-w-[60ch] text-lg font-medium">{summary}</p>
				<p class="mt-1 max-w-prose text-granite-dark">
					Heimdall's rules, run over the real on-chain history of this incident.
				</p>
			{/if}

			<h2 class="mt-10 text-xl font-semibold">Data source</h2>
			<dl class="mt-3 grid max-w-3xl gap-x-8 gap-y-2 tabular sm:grid-cols-[12rem_minmax(0,1fr)]">
				<dt class="text-granite-dark">Blocks</dt>
				<dd>
					{data.fromBlock} to {data.toBlock}{data.step > 1 ? `, every ${data.step}th block` : ''}
				</dd>
				<dt class="text-granite-dark">Chain id</dt>
				<dd>{data.chainId}</dd>
				<dt class="text-granite-dark">Read from</dt>
				<dd>{data.rpcHost} (archive node)</dd>
				<dt class="text-granite-dark">Generated</dt>
				<dd>{formatDateTime(data.generatedAt)}</dd>
			</dl>
			<p class="mt-3 max-w-prose">
				{#each noteParts as part, i (i)}{#if part.url}<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- external source link --><a
							href={part.text}
							target="_blank"
							rel="noopener noreferrer"
							class="break-words">{part.text}<span class="sr-only"> (opens in a new tab)</span></a
						>{:else}{part.text}{/if}{/each}
			</p>
		{/if}
	</div>
{/if}
