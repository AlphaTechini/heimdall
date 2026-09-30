<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { page } from '$app/state';
	import { resolve } from '$app/paths';
	import { Action } from '$lib/action.svelte';
	import { api } from '$lib/api';
	import { app } from '$lib/app.svelte';
	import { auth } from '$lib/auth.svelte';
	import { bandFor, STATUS_LABEL } from '$lib/band';
	import { describeError } from '$lib/errors';
	import { formatAmount, formatNumber, isZero } from '$lib/format';
	import { createGuardActions } from '$lib/guard-actions.svelte';
	import { live } from '$lib/live.svelte';
	import { criticalLabel, defaultPolicy, warningLabel } from '$lib/policy';
	import { positions } from '$lib/positions.svelte';
	import { toasts } from '$lib/toast.svelte';
	import type { ConfigTarget, Exit, Policy, SignalHistory } from '$lib/types';
	import { wallet } from '$lib/wallet.svelte';
	import ActionStatus from '$lib/components/ActionStatus.svelte';
	import ErrorState from '$lib/components/ErrorState.svelte';
	import ExitReceipt from '$lib/components/ExitReceipt.svelte';
	import HornBand from '$lib/components/HornBand.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import Modal from '$lib/components/Modal.svelte';
	import PolicyForm from '$lib/components/PolicyForm.svelte';
	import SignalChart from '$lib/components/SignalChart.svelte';
	import WalletNotice from '$lib/components/WalletNotice.svelte';

	const targetId = $derived(page.params.targetId ?? '');
	const target = $derived(app.target(targetId));
	const position = $derived(positions.position(targetId));
	const snapshot = $derived(live.snapshots[targetId]);
	const band = $derived(position ? bandFor(position, snapshot) : null);

	// ---- signal history ----
	let history = $state<SignalHistory | null>(null);
	let historyStatus = $state<'loading' | 'ready' | 'error'>('loading');
	let historyError = $state('');

	async function loadHistory(id: string, signal?: AbortSignal) {
		historyStatus = 'loading';
		try {
			const res = await api.history(id, 200, signal);
			if (signal?.aborted) return;
			history = res;
			historyStatus = 'ready';
		} catch (e) {
			if (signal?.aborted) return;
			historyError = describeError(e, 'Could not load the signal history.').message;
			historyStatus = 'error';
		}
	}

	$effect(() => {
		const id = targetId;
		const controller = new AbortController();
		void loadHistory(id, controller.signal);
		untrack(() => void live.loadSnapshot(id));
		return () => controller.abort();
	});

	// Live readings extend the chart between page loads.
	$effect(() => {
		const snap = snapshot;
		if (!snap || !history) return;
		untrack(() => {
			const last = history!.points[history!.points.length - 1];
			if (last && last.block >= snap.block) return;
			history!.points.push({
				block: snap.block,
				time: snap.updatedAt,
				severity: snap.severity,
				signals: snap.signals.map((s) => ({ id: s.id, level: s.level, value: s.value }))
			});
			if (history!.points.length > 400) history!.points.shift();
		});
	});

	// ---- exit receipts ----
	let exits = $state<Exit[]>([]);
	let exitsStatus = $state<'loading' | 'ready' | 'error'>('loading');
	let exitsError = $state('');

	async function loadExits(id: string, address: string | null) {
		if (!address) {
			exits = [];
			exitsStatus = 'ready';
			return;
		}
		exitsStatus = 'loading';
		try {
			const res = await api.activity({ address, targetId: id, limit: 100 });
			const wanted = res.events.flatMap((e) => (e.exitId === null ? [] : [e.exitId]));
			const last = positions.position(id)?.lastExit;
			if (last) wanted.push(last.id);
			const ids = wanted.filter((n, i) => wanted.indexOf(n) === i);
			const loaded = await Promise.all(ids.map((n) => api.exit(n)));
			exits = loaded.sort((a, b) => b.id - a.id);
			exitsStatus = 'ready';
		} catch (e) {
			exitsError = describeError(e, 'Could not load exit receipts.').message;
			exitsStatus = 'error';
		}
	}

	$effect(() => {
		const id = targetId;
		const address = wallet.address;
		untrack(() => void loadExits(id, address));
	});

	function upsertExit(x: Exit) {
		if (x.targetId !== targetId) return;
		const i = exits.findIndex((e) => e.id === x.id);
		if (i >= 0) exits[i] = x;
		else exits = [x, ...exits];
	}

	onMount(() => {
		const stops = [
			live.onExit(upsertExit),
			live.onEvent((e) => {
				if (e.targetId === targetId && e.exitId !== null && !exits.some((x) => x.id === e.exitId)) {
					api.exit(e.exitId).then(upsertExit, () => {});
				}
			}),
			live.onReconnect(() => {
				void loadHistory(targetId);
				void loadExits(targetId, wallet.address);
			})
		];
		return () => stops.forEach((s) => s());
	});

	// ---- actions ----
	const actions = createGuardActions(() => target as ConfigTarget);
	const guarded = $derived(position?.status === 'guarded' || position?.status === 'exiting');
	// Keep the outcome of an action on screen even when it changes the position (an exit empties the Guard).
	const showFeedback = $derived(
		[actions.exit, actions.pause, actions.toggle, actions.withdraw].some((a) => a.state !== 'idle')
	);
	const nothingHeld = $derived(position ? isZero(position.guardedPositionTokens) : true);

	// ---- policy editing ----
	let editing = $state(false);
	let draft = $state<Policy>(defaultPolicy());
	let draftValid = $state(true);

	function openEditor() {
		draft = { ...(position?.policy ?? defaultPolicy(app.config?.defaultTipCapUsd ?? 2)) };
		save.reset();
		editing = true;
	}

	const save = new Action('Could not save the policy.', async (ctx) => {
		const guard = positions.guard;
		if (!guard) throw new Error('No Guard found for this wallet. Reload the page.');
		if (!auth.token) ctx.pending('Waiting for wallet signature to sign in');
		await auth.ensure();
		ctx.pending('Saving your policy');
		await auth.call((token) => api.putPolicy(token, guard, targetId, draft));
		await positions.load();
		ctx.success('Policy saved.');
		toasts.show('Policy saved');
		editing = false;
	});

	const retryPositions = () => positions.load();
</script>

<svelte:head
	><title>{target ? `${target.label} - Heimdall` : 'Position - Heimdall'}</title></svelte:head
>

<p><a href={resolve('/')} class="font-medium">Back to your positions</a></p>

{#if !target}
	<h1 class="mt-4 text-3xl display">Position not found</h1>
	<p class="mt-2 max-w-prose">
		Heimdall does not support a position called "{targetId}". Go back to your positions and pick one
		from the list.
	</p>
{:else if !wallet.connected}
	<h1 class="mt-4 text-3xl display sm:text-4xl">{target.label}</h1>
	<p class="mt-2 max-w-prose">Connect your wallet to see this position.</p>
	<div class="mt-4"><WalletNotice /></div>
{:else if positions.status === 'loading' || positions.status === 'idle'}
	<div class="mt-4 max-w-3xl space-y-4" aria-hidden="true">
		<div class="h-10 w-1/2 skeleton"></div>
		<div class="h-28 w-full skeleton"></div>
		<div class="h-40 w-full skeleton"></div>
	</div>
	<p class="sr-only" role="status">Loading position</p>
{:else if positions.status === 'error'}
	<div class="mt-4">
		<ErrorState
			title="This position did not load"
			message={positions.error}
			onretry={retryPositions}
		/>
	</div>
{:else if !position}
	<h1 class="mt-4 text-3xl display sm:text-4xl">{target.label}</h1>
	<p class="mt-2 max-w-prose">
		Your wallet has no {target.label} position. Deposit into it, then come back.
	</p>
{:else}
	<h1 class="mt-4 text-3xl display sm:text-4xl">{target.label}</h1>

	<div class="mt-5 max-w-3xl overflow-hidden rounded-lg border border-line bg-white">
		{#if band}
			<HornBand word={band.word} tone={band.tone}>
				<p class="font-medium">{STATUS_LABEL[position.status]}</p>
				{#if snapshot && snapshot.severity !== 'watch' && position.status !== 'exited'}
					<p class="mt-0.5">{snapshot.reason}</p>
				{/if}
			</HornBand>
		{/if}
		<dl class="grid gap-x-8 gap-y-3 px-5 py-5 tabular sm:grid-cols-2 sm:px-6">
			<div>
				<dt class="text-sm text-granite-dark">In your Guard</dt>
				<dd class="text-2xl font-semibold">
					{formatAmount(position.guardedAmount, target.assetDecimals)}
					<span class="text-lg font-medium">{target.assetSymbol}</span>
				</dd>
			</div>
			<div>
				<dt class="text-sm text-granite-dark">Returned to your wallet by exits</dt>
				<dd class="text-2xl font-semibold">
					{formatAmount(position.returnedAmount, target.assetDecimals)}
					<span class="text-lg font-medium">{target.assetSymbol}</span>
				</dd>
			</div>
			<div>
				<dt class="text-sm text-granite-dark">Still in your wallet, not protected</dt>
				<dd class="text-lg font-semibold">
					{formatAmount(position.walletAmount, target.assetDecimals)}
					{target.assetSymbol}
				</dd>
			</div>
			<div>
				<dt class="text-sm text-granite-dark">Could leave right now</dt>
				<dd class="text-lg font-semibold">
					{formatAmount(position.exitableAmount, target.assetDecimals)}
					{target.assetSymbol}
				</dd>
			</div>
		</dl>
		{#if guarded}
			<p class="flex items-start gap-2 border-t border-line px-5 py-4 font-medium sm:px-6">
				<span class="mt-0.5 text-fjord"><Icon name="shield" size={20} /></span>
				Heimdall can only send this money back to you.
			</p>
		{/if}
	</div>

	<section class="mt-10 max-w-3xl" aria-labelledby="policy-title">
		<h2 id="policy-title" class="text-2xl display">Policy</h2>
		{#if position.status === 'unprotected'}
			<p class="mt-2">
				This position is not protected yet. Start from the
				<a href={resolve('/')}>dashboard</a> and choose Protect.
			</p>
		{:else}
			{#if position.policy}
				<dl class="mt-3 grid gap-x-8 gap-y-2 sm:grid-cols-[14rem_minmax(0,1fr)]">
					<dt class="text-granite-dark">When risk is Critical</dt>
					<dd>{criticalLabel(position.policy.onCritical)}</dd>
					<dt class="text-granite-dark">When risk is Warning</dt>
					<dd>{warningLabel(position.policy.onWarning)}</dd>
					<dt class="text-granite-dark">Send my money to</dt>
					<dd>My wallet, as USDC</dd>
					<dt class="text-granite-dark">Priority exit</dt>
					<dd class="tabular">
						{position.policy.priorityExit
							? `On, up to $${formatNumber(position.policy.tipCapUsd)} extra`
							: 'Off'}
					</dd>
				</dl>
			{:else}
				<p class="mt-2">
					No policy is saved for this position, so your default policy from Settings applies.
				</p>
			{/if}
			<div class="mt-4">
				<button
					type="button"
					class="rounded-md border border-granite px-5 py-2.5 font-medium hover:bg-frost"
					onclick={openEditor}
				>
					Edit policy
				</button>
			</div>
		{/if}
	</section>

	{#if guarded || showFeedback}
		<section class="mt-10 max-w-3xl" aria-labelledby="actions-title">
			<h2 id="actions-title" class="text-2xl display">Actions</h2>
			{#if guarded}
				<p class="mt-2 text-granite-dark">
					These are wallet transactions. Only you can send them, and each one takes effect in the
					same block.
				</p>
				{#if wallet.issue}
					<div class="mt-3"><WalletNotice /></div>
				{/if}
				<div class="mt-4 flex flex-wrap gap-3">
					<button
						type="button"
						class="rounded-md bg-fjord px-5 py-2.5 font-medium text-white hover:bg-fjord-dark disabled:opacity-60"
						disabled={actions.busy || !!wallet.issue || nothingHeld}
						onclick={() => actions.exit.run()}
					>
						Exit now
					</button>
					<button
						type="button"
						class="rounded-md border border-granite px-5 py-2.5 font-medium hover:bg-frost disabled:opacity-60"
						disabled={actions.busy || !!wallet.issue}
						onclick={() => actions.pause.run()}
					>
						{positions.paused ? 'Resume' : 'Pause Heimdall'}
					</button>
					<button
						type="button"
						class="rounded-md border border-granite px-5 py-2.5 font-medium hover:bg-frost disabled:opacity-60"
						disabled={actions.busy || !!wallet.issue}
						onclick={() => actions.toggle.run()}
					>
						{positions.keeperEnabled ? 'Turn off protection' : 'Turn on protection'}
					</button>
					<button
						type="button"
						class="rounded-md border border-granite px-5 py-2.5 font-medium hover:bg-frost disabled:opacity-60"
						disabled={actions.busy || !!wallet.issue || nothingHeld}
						onclick={() => actions.withdraw.run()}
					>
						Withdraw to my wallet
					</button>
				</div>
				{#if nothingHeld}
					<p class="mt-3 text-sm text-granite-dark">
						Exit now and Withdraw are unavailable because your Guard holds nothing for this
						position.
					</p>
				{/if}
			{/if}
			<div class="mt-3 space-y-2">
				<ActionStatus action={actions.exit} />
				<ActionStatus action={actions.pause} />
				<ActionStatus action={actions.toggle} />
				<ActionStatus action={actions.withdraw} />
			</div>
			{#if guarded}
				<p class="mt-3 max-w-prose text-sm text-granite-dark">
					Exit now redeems as much as the vault can pay today and sends it to your wallet. Pause
					stops Heimdall from exiting for you; your own actions still work.
				</p>
			{/if}
		</section>
	{/if}

	<section class="mt-10" aria-labelledby="history-title">
		<h2 id="history-title" class="text-2xl display">Signal history</h2>
		<p class="mt-2 max-w-prose text-granite-dark">
			What each risk signal read over time. Dashed lines mark where it becomes Warning or Critical.
		</p>
		<div class="mt-4">
			{#if historyStatus === 'loading'}
				<div class="grid gap-6 sm:grid-cols-2 lg:grid-cols-3" aria-hidden="true">
					{#each [0, 1, 2, 3, 4, 5] as i (i)}
						<div class="h-40 skeleton"></div>
					{/each}
				</div>
				<p class="sr-only" role="status">Loading signal history</p>
			{:else if historyStatus === 'error'}
				<ErrorState
					title="Signal history did not load"
					message={historyError}
					onretry={() => loadHistory(targetId)}
				/>
			{:else if !history || history.points.length === 0}
				<p class="text-granite-dark">
					No history yet. Readings appear here once Heimdall has watched this vault for a while.
				</p>
			{:else}
				<div class="grid gap-6 sm:grid-cols-2 lg:grid-cols-3">
					{#each app.config?.signals ?? [] as info (info.id)}
						<SignalChart
							id={info.id}
							label={info.label}
							tooltip={info.tooltip}
							unit={snapshot?.signals.find((s) => s.id === info.id)?.unit ?? ''}
							points={history.points}
							threshold={history.thresholds[info.id]}
						/>
					{/each}
				</div>
			{/if}
		</div>
	</section>

	<section id="exits" class="mt-10 max-w-3xl scroll-mt-6" aria-labelledby="exits-title">
		<h2 id="exits-title" class="text-2xl display">Exit receipts</h2>
		<div class="mt-3">
			{#if exitsStatus === 'loading'}
				<div class="space-y-3" aria-hidden="true">
					<div class="h-6 w-2/3 skeleton"></div>
					<div class="h-20 w-full skeleton"></div>
				</div>
				<p class="sr-only" role="status">Loading exit receipts</p>
			{:else if exitsStatus === 'error'}
				<ErrorState
					title="Exit receipts did not load"
					message={exitsError}
					onretry={() => loadExits(targetId, wallet.address)}
				/>
			{:else if exits.length === 0}
				<p class="text-granite-dark">
					No exits yet. If Heimdall or you take this money out of the vault, the receipt appears
					here with the reason, amounts, blocks and links.
				</p>
			{:else}
				<div>
					{#each exits as exit (exit.id)}
						<ExitReceipt {exit} {target} />
					{/each}
				</div>
			{/if}
		</div>
	</section>

	{#if editing}
		<Modal title="Edit policy" onclose={() => (editing = false)} locked={save.busy}>
			<p class="mb-5 text-granite-dark">{target.label}</p>
			<PolicyForm
				bind:policy={draft}
				bind:valid={draftValid}
				safeAddress={wallet.address}
				disabled={save.busy}
			/>

			{#snippet footer()}
				{#if wallet.issue}
					<WalletNotice class="mb-3" />
				{/if}
				<ActionStatus action={save} class="mb-3" />
				<div class="flex flex-wrap gap-3">
					<button
						type="button"
						class="rounded-md bg-fjord px-5 py-2.5 font-medium text-white hover:bg-fjord-dark disabled:opacity-60"
						disabled={save.busy || !draftValid || !!wallet.issue}
						onclick={() => save.run()}
					>
						Save policy
					</button>
					<button
						type="button"
						class="rounded-md border border-granite px-5 py-2.5 font-medium hover:bg-frost disabled:opacity-60"
						disabled={save.busy}
						onclick={() => (editing = false)}
					>
						Cancel
					</button>
				</div>
				{#if !draftValid}
					<p class="mt-2 text-sm">Fix the priority limit before you save.</p>
				{/if}
			{/snippet}
		</Modal>
	{/if}
{/if}
