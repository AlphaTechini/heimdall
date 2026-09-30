<script lang="ts">
	import { txLink } from '$lib/explorer';
	import { formatAmount, formatDateTime, formatNumber, formatUsd } from '$lib/format';
	import type { ConfigTarget, Exit } from '$lib/types';
	import ExplorerLink from './ExplorerLink.svelte';

	let { exit, target }: { exit: Exit; target: ConfigTarget } = $props();

	const STATUS: Record<Exit['status'], string> = {
		active: 'In progress',
		complete: 'Complete',
		stopped: 'Stopped by you',
		timeout: 'Timed out before everything came out',
		failed: 'Failed'
	};
	const TRIGGER: Record<Exit['trigger'], string> = {
		auto: 'Heimdall, automatically',
		manual_keeper: 'Heimdall, on a manual trigger',
		owner: 'You'
	};
	const RESULT: Record<string, string> = {
		exited: 'Money sent to your wallet',
		deferred: 'Vault had no cash, will retry',
		reverted: 'Reverted',
		pending: 'Waiting to be mined',
		replaced: 'Replaced by a higher tip'
	};
	const blocks = $derived(
		exit.endBlock === null
			? `${exit.startBlock}, still running`
			: exit.endBlock === exit.startBlock
				? `${exit.startBlock}`
				: `${exit.startBlock} to ${exit.endBlock}`
	);
	const amount = (units: string) =>
		`${formatAmount(units, target.assetDecimals)} ${target.assetSymbol}`;
</script>

<article class="border-t border-line py-5 first:border-t-0">
	<div class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
		<h3 class="text-lg font-semibold">Exit {exit.id}: {STATUS[exit.status]}</h3>
		<time class="text-sm text-granite-dark tabular" datetime={exit.decidedAt}
			>{formatDateTime(exit.decidedAt)}</time
		>
	</div>
	<p class="mt-1">{exit.reason}</p>

	<dl class="mt-3 grid gap-x-8 gap-y-1 text-sm tabular sm:grid-cols-[13rem_minmax(0,1fr)]">
		<dt class="text-granite-dark">Started by</dt>
		<dd>{TRIGGER[exit.trigger]}</dd>
		<dt class="text-granite-dark">Returned to your wallet</dt>
		<dd class="font-semibold">{amount(exit.totalOut)}</dd>
		<dt class="text-granite-dark">Blocks</dt>
		<dd>{blocks}</dd>
		<dt class="text-granite-dark">Decision to broadcast</dt>
		<dd>
			{exit.decisionToBroadcastMs === null
				? 'Not measured'
				: `${formatNumber(exit.decisionToBroadcastMs, 0)} ms`}
		</dd>
		<dt class="text-granite-dark">Tip limit</dt>
		<dd>{formatUsd(exit.tipCapUsd)}</dd>
	</dl>

	{#if exit.txs.length > 0}
		<h4 class="mt-4 text-sm font-semibold">Transactions</h4>
		<ul class="mt-1 divide-y divide-line text-sm">
			{#each exit.txs as tx, i (`${i}-${tx.hash}`)}
				<li class="flex flex-wrap items-baseline gap-x-5 gap-y-0.5 py-2 tabular">
					<span>Block {tx.block}</span>
					<span>{amount(tx.amountOut)} out</span>
					<span>{amount(tx.remaining)} left</span>
					<span>Tip {formatUsd(tx.tipUsd)}</span>
					<span class="text-granite-dark">{RESULT[tx.result] ?? tx.result}</span>
					<ExplorerLink link={txLink(tx.hash)}>View transaction</ExplorerLink>
				</li>
			{/each}
		</ul>
	{/if}
</article>
