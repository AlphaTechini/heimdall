<script lang="ts">
	import { formatGwei, formatUnits } from 'viem';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { api } from '$lib/api';
	import { describeError } from '$lib/errors';
	import type { TxInfo } from '$lib/types';
	import ErrorState from '$lib/components/ErrorState.svelte';

	const hash = $derived(page.params.hash ?? '');
	let tx = $state<TxInfo | null>(null);
	let status = $state<'loading' | 'ready' | 'error'>('loading');
	let error = $state('');

	async function load(h: string, signal?: AbortSignal) {
		status = 'loading';
		try {
			const res = await api.tx(h, signal);
			if (signal?.aborted) return;
			tx = res;
			status = 'ready';
		} catch (e) {
			if (signal?.aborted) return;
			error = describeError(e, 'Could not load this transaction.').message;
			status = 'error';
		}
	}

	$effect(() => {
		const controller = new AbortController();
		void load(hash, controller.signal);
		return () => controller.abort();
	});

	function gwei(wei: string) {
		return `${formatGwei(BigInt(wei))} gwei`;
	}
	function show(value: unknown): string {
		if (typeof value === 'string') return value;
		return JSON.stringify(value, (_k, v) => (typeof v === 'bigint' ? v.toString() : v));
	}
</script>

<svelte:head><title>Transaction - Heimdall</title></svelte:head>

<h1 class="text-3xl display sm:text-4xl">Transaction</h1>
<p class="mt-2 max-w-full text-sm break-all text-granite-dark tabular">{hash}</p>

<div class="mt-6">
	{#if status === 'loading'}
		<div class="max-w-2xl space-y-3" aria-hidden="true">
			{#each [0, 1, 2, 3, 4] as i (i)}
				<div class="h-6 w-full skeleton"></div>
			{/each}
		</div>
		<p class="sr-only" role="status">Loading transaction</p>
	{:else if status === 'error'}
		<ErrorState title="This transaction did not load" message={error} onretry={() => load(hash)} />
	{:else if tx}
		<dl class="grid max-w-3xl gap-x-8 gap-y-3 tabular sm:grid-cols-[12rem_minmax(0,1fr)]">
			<dt class="text-granite-dark">Status</dt>
			<dd class="font-semibold">{tx.status === 'success' ? 'Succeeded' : 'Reverted'}</dd>
			<dt class="text-granite-dark">Block</dt>
			<dd>{tx.blockNumber}</dd>
			<dt class="text-granite-dark">From</dt>
			<dd class="break-all">{tx.from}</dd>
			<dt class="text-granite-dark">To</dt>
			<dd class="break-all">{tx.to ?? 'Contract creation'}</dd>
			<dt class="text-granite-dark">Priority fee</dt>
			<dd>{gwei(tx.maxPriorityFeePerGas)} per gas</dd>
			<dt class="text-granite-dark">Gas price paid</dt>
			<dd>{gwei(tx.effectiveGasPrice)}</dd>
			<dt class="text-granite-dark">Gas used</dt>
			<dd>{formatUnits(BigInt(tx.gasUsed), 0)}</dd>
		</dl>

		<h2 class="mt-10 text-xl display">Heimdall events</h2>
		{#if tx.logs.filter((l) => l.name).length === 0}
			<p class="mt-2 text-granite-dark">This transaction did not emit any Heimdall events.</p>
		{:else}
			<ul class="mt-3 max-w-3xl divide-y divide-line border-y border-line">
				{#each tx.logs.filter((l) => l.name) as log, i (i)}
					<li class="py-3">
						<p class="font-semibold">{log.name}</p>
						<p class="text-sm break-all text-granite-dark tabular">{log.address}</p>
						{#if log.args}
							<dl
								class="mt-2 grid gap-x-6 gap-y-1 text-sm tabular sm:grid-cols-[10rem_minmax(0,1fr)]"
							>
								{#each Object.entries(log.args) as [key, value] (key)}
									<dt class="text-granite-dark">{key}</dt>
									<dd class="break-all">{show(value)}</dd>
								{/each}
							</dl>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	{/if}
</div>

<p class="mt-8"><a href={resolve('/activity')} class="font-medium">Back to activity</a></p>
