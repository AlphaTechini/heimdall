<script lang="ts">
	import { onMount } from 'svelte';
	import { resolve } from '$app/paths';
	import { ActivityFeed } from '$lib/activity.svelte';
	import { app } from '$lib/app.svelte';
	import { positions } from '$lib/positions.svelte';
	import { wallet } from '$lib/wallet.svelte';
	import type { ConfigTarget } from '$lib/types';
	import ErrorState from '$lib/components/ErrorState.svelte';
	import EventFeedPanel from '$lib/components/EventFeedPanel.svelte';
	import PositionCard from '$lib/components/PositionCard.svelte';
	import ProtectPanel from '$lib/components/ProtectPanel.svelte';
	import WalletNotice from '$lib/components/WalletNotice.svelte';
	import { live } from '$lib/live.svelte';

	const feed = new ActivityFeed(10, true);
	let protecting = $state<ConfigTarget | null>(null);
	let retrying = $state(false);

	onMount(() => feed.listen());

	// The rail follows the wallet: its own events when connected, the global feed otherwise.
	$effect(() => {
		const address = wallet.address;
		void feed.load(address, null);
	});

	// Live risk readings for every position on screen.
	$effect(() => {
		for (const p of positions.data?.positions ?? []) {
			if (!live.snapshots[p.targetId] && !live.snapshotErrors[p.targetId]) {
				void live.loadSnapshot(p.targetId);
			}
		}
	});

	// Before a wallet connects, the vault list shows what Heimdall sees on each vault right now.
	$effect(() => {
		if (wallet.connected) return;
		for (const t of app.config?.targets ?? []) {
			if (!live.snapshots[t.id] && !live.snapshotErrors[t.id]) void live.loadSnapshot(t.id);
		}
	});
	const WATCH: Record<string, { word: string; dot: string }> = {
		watch: { word: 'Calm', dot: 'bg-calm' },
		warning: { word: 'Warning', dot: 'bg-amber' },
		critical: { word: 'Critical', dot: 'bg-ember' }
	};

	const cards = $derived(
		(positions.data?.positions ?? [])
			.map((position) => ({ position, target: app.target(position.targetId) }))
			.filter((c): c is { position: typeof c.position; target: ConfigTarget } => !!c.target)
	);

	async function retry() {
		retrying = true;
		await positions.load();
		retrying = false;
	}
</script>

<svelte:head><title>Dashboard - Heimdall</title></svelte:head>

<div class="grid gap-8 lg:grid-cols-[minmax(0,1fr)_22rem] lg:gap-10">
	<div class="min-w-0">
		<h1 class="title">Your positions</h1>
		<p class="mt-3 max-w-[58ch] text-[1.0625rem] text-granite-dark">
			Heimdall watches your money in Arbitrum lending vaults. If an attack starts, it moves your
			money back to your own wallet.
		</p>

		<div class="mt-8 space-y-6">
			{#if !wallet.connected}
				<section aria-labelledby="connect-title" class="overflow-hidden panel">
					<div class="px-6 py-6 sm:px-7">
						<h2 id="connect-title" class="text-xl font-semibold">Connect your wallet to begin</h2>
						<p class="mt-2 max-w-[60ch] text-granite-dark">
							Heimdall finds your positions in the vaults below and shows how risky each one is
							right now. It never asks for your keys.
						</p>
						<div class="mt-5">
							{#if wallet.issue?.kind === 'no_wallet'}
								<WalletNotice />
							{:else}
								<button
									type="button"
									class="btn btn-primary"
									disabled={wallet.connecting}
									onclick={() => wallet.connect()}
								>
									{wallet.connecting ? 'Waiting for wallet' : 'Connect wallet'}
								</button>
							{/if}
						</div>
					</div>
					<div class="border-t border-line bg-well px-6 py-5 sm:px-7">
						<h3 class="text-sm font-semibold">Vaults Heimdall watches</h3>
						<ul class="mt-3 divide-y divide-line">
							{#each app.config?.targets ?? [] as t (t.id)}
								{@const sev = WATCH[live.snapshots[t.id]?.severity ?? '']}
								<li class="flex items-center justify-between gap-4 py-2.5">
									<span class="min-w-0">
										<span class="block font-medium">{t.label}</span>
										<span class="mt-0.5 flex items-center gap-1.5 text-sm text-granite-dark">
											<span
												class="size-2 rounded-full {sev?.dot ?? 'bg-granite'}"
												aria-hidden="true"
											></span>
											{sev ? `${sev.word}, watched every block` : 'Checking'}
										</span>
									</span>
									<span class="flex items-center gap-2 text-sm text-granite-dark">
										{t.protocol}
										<span
											class="rounded-md border border-line bg-white px-1.5 py-0.5 text-xs font-semibold text-ink"
											>{t.assetSymbol}</span
										>
									</span>
								</li>
							{/each}
						</ul>
					</div>
				</section>
			{:else if positions.status === 'loading' || positions.status === 'idle'}
				{#each [0, 1] as i (i)}
					<div class="overflow-hidden panel" aria-hidden="true">
						<div class="h-[6.5rem] skeleton rounded-none"></div>
						<div class="space-y-4 px-6 py-5">
							<div class="h-5 w-1/2 skeleton"></div>
							<div class="h-9 w-1/3 skeleton"></div>
							<div class="grid grid-cols-2 gap-2 sm:grid-cols-3">
								{#each [0, 1, 2, 3, 4, 5] as j (j)}
									<div class="h-[5.25rem] skeleton"></div>
								{/each}
							</div>
						</div>
					</div>
				{/each}
				<p class="sr-only" role="status">Loading your positions</p>
			{:else if positions.status === 'error'}
				<ErrorState
					title="Your positions did not load"
					message={positions.error}
					onretry={retry}
					{retrying}
				/>
			{:else if cards.length === 0}
				<section aria-labelledby="empty-title" class="panel px-6 py-6 sm:px-7">
					<h2 id="empty-title" class="text-xl font-semibold">
						No supported positions in this wallet
					</h2>
					<p class="mt-2">Deposit into a supported vault, then come back.</p>
					<ul class="mt-4 divide-y divide-line border-y border-line">
						{#each app.config?.targets ?? [] as t (t.id)}
							<li class="py-2">{t.label}</li>
						{/each}
					</ul>
				</section>
			{:else}
				{#if wallet.issue}
					<WalletNotice />
				{/if}
				{#each cards as { position, target } (position.targetId)}
					<PositionCard {target} {position} onprotect={(t) => (protecting = t)} />
				{/each}
			{/if}
		</div>
	</div>

	<aside aria-labelledby="rail-title" class="min-w-0 self-start panel px-5 py-5 lg:sticky lg:top-6">
		<h2 id="rail-title" class="text-lg font-semibold">Activity</h2>
		<div class="mt-3">
			<EventFeedPanel
				{feed}
				compact
				emptyText={wallet.connected
					? 'Nothing yet. Risk changes, alerts and exits for your positions appear here.'
					: 'Nothing yet. Risk changes, alerts and exits appear here.'}
			/>
		</div>
		<p class="mt-4 border-t border-line pt-4 text-sm">
			<a href={resolve('/activity')} class="font-medium">See all activity</a>
		</p>
	</aside>
</div>

{#if protecting}
	<ProtectPanel target={protecting} onclose={() => (protecting = null)} />
{/if}
