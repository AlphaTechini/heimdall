<script lang="ts">
	import { resolve } from '$app/paths';
	import { bandFor, STATUS_LABEL } from '$lib/band';
	import { formatAmount, isZero } from '$lib/format';
	import { createGuardActions } from '$lib/guard-actions.svelte';
	import { live } from '$lib/live.svelte';
	import { positions } from '$lib/positions.svelte';
	import type { ConfigTarget, Position } from '$lib/types';
	import { wallet } from '$lib/wallet.svelte';
	import ActionStatus from './ActionStatus.svelte';
	import HornBand from './HornBand.svelte';
	import Icon from './Icon.svelte';
	import RiskStrip from './RiskStrip.svelte';

	let {
		target,
		position,
		onprotect
	}: { target: ConfigTarget; position: Position; onprotect: (target: ConfigTarget) => void } =
		$props();

	const snapshot = $derived(live.snapshots[target.id]);
	const band = $derived(bandFor(position, snapshot));
	const keeperOn = $derived(positions.keeperEnabled);
	const uid = $props.id();

	const shown = $derived.by(() => {
		switch (position.status) {
			case 'unprotected':
				// The wallet holds vault shares or aTokens: the money itself sits in the protocol.
				return { units: position.walletAmount, note: `in ${target.label}, not protected` };
			case 'exited':
				return { units: position.returnedAmount, note: 'returned to your wallet' };
			default:
				return { units: position.guardedAmount, note: 'in your Guard' };
		}
	});

	// Unprotected only: how much of it could come out right now. Under 1% means stuck.
	const withdrawable = $derived(
		position.status === 'unprotected' && position.walletWithdrawable !== null
			? position.walletWithdrawable
			: null
	);
	const stuck = $derived.by(() => {
		if (withdrawable === null) return false;
		try {
			const held = BigInt(position.walletAmount || '0');
			return held > 0n && BigInt(withdrawable) * 100n < held;
		} catch {
			return false;
		}
	});

	const actions = createGuardActions(() => target);
	const toggle = actions.toggle;
	const withdraw = actions.withdraw;

	const returnedNote = $derived(
		position.status === 'exiting'
			? `, ${formatAmount(position.returnedAmount, target.assetDecimals)} ${target.assetSymbol} already returned`
			: ''
	);
	const nothingToWithdraw = $derived(isZero(position.guardedPositionTokens));
	const busy = $derived(actions.busy);
</script>

<article class="overflow-hidden panel" aria-labelledby="{uid}-title">
	<HornBand word={band.word} tone={band.tone}>
		{#if STATUS_LABEL[position.status] !== band.word}
			<p class="font-medium">{STATUS_LABEL[position.status]}</p>
		{/if}
		{#if snapshot && snapshot.severity !== 'watch' && position.status !== 'exited'}
			<p class="mt-0.5">{snapshot.reason}</p>
		{/if}
	</HornBand>

	<div class="space-y-6 px-6 py-6 sm:px-7">
		<div>
			<h2 id="{uid}-title" class="text-base font-semibold tracking-normal text-granite-dark">
				{target.label}
			</h2>
			<p class="mt-1.5 text-[2.5rem] leading-none font-semibold tracking-[-0.03em] tabular">
				{formatAmount(shown.units, target.assetDecimals)}
				<span class="text-xl font-medium tracking-normal text-granite-dark"
					>{target.assetSymbol}</span
				>
			</p>
			<p class="mt-2 text-sm text-granite-dark">
				{shown.note}{returnedNote}
			</p>
			{#if withdrawable !== null}
				<p class="mt-3 flex flex-wrap items-baseline gap-x-2 text-sm">
					<span class="text-granite-dark">Withdrawable now</span>
					<span class="font-semibold tabular {stuck ? 'text-ember-dark' : ''}">
						{formatAmount(withdrawable, target.assetDecimals)}
						{target.assetSymbol}
					</span>
				</p>
				{#if stuck}
					<p class="mt-1 text-sm font-medium text-ember-dark">
						Stuck: the vault has no cash to pay this out.
					</p>
				{/if}
			{/if}
		</div>

		{#if position.status !== 'exited'}
			<RiskStrip targetId={target.id} />
		{/if}

		{#if position.status === 'guarded' || position.status === 'exiting'}
			<p class="flex items-start gap-2.5 rounded-xl bg-well px-4 py-3 font-medium">
				<span class="mt-0.5 text-fjord"><Icon name="shield" size={20} /></span>
				Heimdall can only send this money back to you.
			</p>
			{#if !keeperOn}
				<p class="text-sm">
					Protection is off for your Guard. Heimdall will not exit this position until you turn it
					back on.
				</p>
			{:else if positions.paused}
				<p class="text-sm">Heimdall is paused for your Guard. Open the details to resume it.</p>
			{/if}
		{/if}

		{#if position.status === 'exited'}
			<p class="text-sm text-granite-dark">
				Heimdall exited this position. The money went to your wallet.
			</p>
		{/if}

		<div class="flex flex-wrap gap-3 max-sm:[&>*]:w-full max-sm:[&>a]:text-center">
			{#if position.status === 'unprotected'}
				<button type="button" class="btn btn-primary" onclick={() => onprotect(target)}>
					Protect
				</button>
			{:else if position.status === 'guarded'}
				<a href={resolve('/positions/[targetId]', { targetId: target.id })} class="btn btn-primary">
					View details
				</a>
				<button
					type="button"
					class="btn btn-secondary"
					disabled={busy || !!wallet.issue}
					onclick={() => toggle.run()}
				>
					{keeperOn ? 'Turn off protection' : 'Turn on protection'}
				</button>
				<button
					type="button"
					class="btn btn-secondary"
					disabled={busy || !!wallet.issue || nothingToWithdraw}
					onclick={() => withdraw.run()}
				>
					Withdraw to my wallet
				</button>
			{:else if position.status === 'exiting'}
				<a href={resolve('/positions/[targetId]', { targetId: target.id })} class="btn btn-primary">
					View details
				</a>
			{:else}
				<a
					href={resolve('/positions/[targetId]#exits', { targetId: target.id })}
					class="btn btn-primary"
				>
					View exit
				</a>
			{/if}
		</div>

		{#if position.status === 'guarded'}
			{#if wallet.issue}
				<p class="text-sm text-granite-dark">
					Turn off and Withdraw need your wallet: {wallet.issue.message}
				</p>
			{:else if nothingToWithdraw}
				<p class="text-sm text-granite-dark">
					Withdraw is unavailable because your Guard holds nothing for this position.
				</p>
			{/if}
			<ActionStatus action={toggle} />
			<ActionStatus action={withdraw} />
		{/if}
	</div>
</article>
