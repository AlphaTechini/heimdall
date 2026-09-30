<script lang="ts">
	import { onMount } from 'svelte';
	import { Action } from '$lib/action.svelte';
	import { api } from '$lib/api';
	import { app } from '$lib/app.svelte';
	import { describeError } from '$lib/errors';
	import { formatAmount, plural } from '$lib/format';
	import { live } from '$lib/live.svelte';
	import type { SimCompare, SimLine, SimScenarios } from '$lib/types';
	import ActionStatus from '$lib/components/ActionStatus.svelte';
	import ErrorState from '$lib/components/ErrorState.svelte';

	const LABEL = 'Simulated on an Arbitrum One fork';
	const uid = $props.id();

	let scenarios = $state<SimScenarios | null>(null);
	let status = $state<'loading' | 'ready' | 'error'>('loading');
	let error = $state('');
	let selected = $state('');
	let lines = $state<SimLine[]>([]);
	let compare = $state<SimCompare | null>(null);
	let compareStatus = $state<'loading' | 'ready' | 'error'>('loading');
	let compareError = $state('');

	async function loadScenarios() {
		try {
			scenarios = await api.simScenarios();
			status = 'ready';
			if (!selected) {
				selected = scenarios.scenarios.find((s) => s.available)?.id ?? '';
			}
		} catch (e) {
			error = describeError(e, 'Could not load the scenarios.').message;
			status = 'error';
		}
	}

	async function loadCompare() {
		compareStatus = 'loading';
		try {
			compare = await api.simCompare();
			compareStatus = 'ready';
		} catch (e) {
			compareError = describeError(e, 'Could not load the comparison.').message;
			compareStatus = 'error';
		}
	}

	onMount(() => {
		if (!app.config?.demoMode) return;
		void loadScenarios();
		void loadCompare();
		const stops = [
			live.onSim((line) => {
				lines = [...lines, line];
				if (line.done) {
					void loadScenarios();
					void loadCompare();
				}
			}),
			live.onReconnect(() => {
				void loadScenarios();
				void loadCompare();
			})
		];
		return () => stops.forEach((s) => s());
	});

	const running = $derived(scenarios?.running ?? null);
	const chosen = $derived(scenarios?.scenarios.find((s) => s.id === selected));
	const unavailable = $derived(scenarios?.scenarios.filter((s) => !s.available) ?? []);

	const run = new Action('Could not start the scenario.', async (ctx) => {
		if (!chosen) throw new Error('Choose a scenario first.');
		ctx.pending(`Starting ${chosen.name}`);
		lines = [];
		await api.simRun(chosen.id);
		await loadScenarios();
		ctx.success(`${chosen.name} started. Follow it in the log below.`);
	});

	const reset = new Action('Could not reset the fork.', async (ctx) => {
		ctx.pending('Restoring the fork to its starting point');
		await api.simReset();
		lines = [];
		await Promise.all([loadScenarios(), loadCompare()]);
		ctx.success('The fork is back to its starting point.');
	});

	const runDisabledReason = $derived(
		running
			? `${scenarios?.scenarios.find((s) => s.id === running)?.name ?? 'A scenario'} is running. Wait for it to finish, or reset the fork.`
			: !chosen
				? 'Choose a scenario to run.'
				: !chosen.available
					? (chosen.reason ?? 'This scenario is not available on this fork.')
					: ''
	);

	const target = $derived(compare ? app.target(compare.targetId) : undefined);
	const money = (units: string) =>
		`${formatAmount(units, target?.assetDecimals ?? 6)} ${target?.assetSymbol ?? ''}`.trim();

	const timeline = $derived.by(() => {
		const t = compare?.timeline;
		if (!t) return [];
		return [
			{ label: 'First signal', block: t.firstSignalBlock },
			{ label: 'Exit submitted', block: t.exitSubmittedBlock },
			{ label: 'Exit confirmed', block: t.exitConfirmedBlock },
			{ label: 'Drain finished', block: t.drainFinishedBlock }
		];
	});
	const span = $derived.by(() => {
		const blocks = timeline.map((r) => r.block).filter((b): b is number => b !== null);
		if (blocks.length < 2) return null;
		return { min: Math.min(...blocks), max: Math.max(...blocks) };
	});
	const caption = $derived.by(() => {
		const t = compare?.timeline;
		if (!t || t.firstSignalBlock === null || t.exitSubmittedBlock === null) return '';
		const after = t.exitSubmittedBlock - t.firstSignalBlock;
		let text = `Heimdall submitted the exit ${plural(after, 'block', 'blocks')} after the first signal.`;
		if (t.exitConfirmedBlock !== null && t.drainFinishedBlock !== null) {
			const more = t.drainFinishedBlock - t.exitConfirmedBlock;
			if (more > 0) text += ` The drain continued for another ${plural(more, 'block', 'blocks')}.`;
		}
		return text;
	});
	function pct(block: number | null) {
		if (block === null || !span) return 0;
		return span.max === span.min ? 0 : ((block - span.min) / (span.max - span.min)) * 100;
	}
</script>

<svelte:head><title>Simulator - Heimdall</title></svelte:head>

<h1 class="text-3xl display sm:text-4xl">Incident simulator</h1>
<p class="mt-2 font-medium">{LABEL}</p>

{#if !app.config?.demoMode}
	<p class="mt-4 max-w-prose">
		The simulator only runs on a local Arbitrum One fork with demo mode on. This server is not in
		demo mode, so there is nothing to run here.
	</p>
{:else if status === 'loading'}
	<div class="mt-6 max-w-xl space-y-3" aria-hidden="true">
		<div class="h-10 w-full skeleton"></div>
		<div class="h-10 w-40 skeleton"></div>
	</div>
	<p class="sr-only" role="status">Loading scenarios</p>
{:else if status === 'error'}
	<div class="mt-6">
		<ErrorState
			title="The simulator did not load"
			message={error}
			onretry={() => {
				status = 'loading';
				void loadScenarios();
			}}
		/>
	</div>
{:else if scenarios}
	<p class="mt-2 max-w-prose text-granite-dark">
		Replay an attack against a vault on the fork and watch Heimdall react. Nothing here touches a
		real network.
	</p>

	<section class="mt-8 max-w-2xl" aria-labelledby="{uid}-run">
		<h2 id="{uid}-run" class="text-2xl display">Run a scenario</h2>
		<label for="{uid}-scenario" class="mt-3 block text-sm font-medium">Scenario</label>
		<select
			id="{uid}-scenario"
			class="mt-1 w-full rounded-md border-granite bg-white py-2"
			bind:value={selected}
			disabled={!!running}
		>
			{#each scenarios.scenarios as s (s.id)}
				<option value={s.id} disabled={!s.available}>
					{s.name}{s.available ? '' : ' (not available)'}
				</option>
			{/each}
		</select>
		{#if chosen}
			<p class="mt-2 text-sm text-granite-dark">{chosen.description}</p>
		{/if}
		{#if unavailable.length > 0}
			<ul class="mt-2 space-y-1 text-sm">
				{#each unavailable as s (s.id)}
					<li><span class="font-medium">{s.name} is not available.</span> {s.reason}</li>
				{/each}
			</ul>
		{/if}

		<div class="mt-4 flex flex-wrap gap-3 max-sm:[&>*]:w-full max-sm:[&>a]:text-center">
			<button
				type="button"
				class="rounded-md bg-fjord px-5 py-2.5 font-medium text-white hover:bg-fjord-dark disabled:opacity-60"
				disabled={run.busy || reset.busy || runDisabledReason !== ''}
				onclick={() => run.run()}
			>
				Run scenario
			</button>
			<button
				type="button"
				class="rounded-md border border-granite px-5 py-2.5 font-medium hover:bg-frost disabled:opacity-60"
				disabled={run.busy || reset.busy}
				onclick={() => reset.run()}
			>
				Reset fork
			</button>
		</div>
		{#if runDisabledReason}
			<p class="mt-2 text-sm">{runDisabledReason}</p>
		{/if}
		<p class="mt-2 text-sm text-granite-dark">
			Reset fork restores the chain to how it was when the server started and clears the exits and
			events made since.
		</p>
		<ActionStatus action={run} class="mt-3" />
		<ActionStatus action={reset} class="mt-2" />
	</section>

	<section class="mt-10 max-w-3xl" aria-labelledby="{uid}-log">
		<h2 id="{uid}-log" class="text-2xl display">Live log</h2>
		<div
			class="mt-3 max-h-80 overflow-y-auto border-y border-line py-2"
			role="log"
			aria-live="polite"
			aria-label="Scenario progress"
		>
			{#if lines.length === 0}
				<p class="text-granite-dark">
					{running
						? 'Waiting for the first line.'
						: 'No run yet. Choose a scenario and select Run scenario.'}
				</p>
			{:else}
				<ol class="space-y-1 text-sm tabular">
					{#each lines as l, i (i)}
						<li>
							{l.line}{#if l.done}&nbsp;<span class="font-semibold">Finished.</span>{/if}
						</li>
					{/each}
				</ol>
			{/if}
		</div>
	</section>

	<section class="mt-10 max-w-3xl" aria-labelledby="{uid}-compare">
		<h2 id="{uid}-compare" class="text-2xl display">Ada and Ben</h2>
		<p class="mt-1 text-sm font-medium">{compare?.label ?? LABEL}</p>
		<div class="mt-3">
			{#if compareStatus === 'loading'}
				<div class="h-40 w-full skeleton" aria-hidden="true"></div>
				<p class="sr-only" role="status">Loading comparison</p>
			{:else if compareStatus === 'error'}
				<ErrorState
					title="The comparison did not load"
					message={compareError}
					onretry={loadCompare}
				/>
			{:else if compare}
				<p class="mb-4 max-w-prose text-granite-dark">
					Both hold the same position in {target?.label ?? compare.targetId}. Ada has Heimdall
					protection. Ben does not.
				</p>
				<div class="grid gap-6 sm:grid-cols-2">
					{#each [{ name: 'Ada, protected', party: compare.ada }, { name: 'Ben, unprotected', party: compare.ben }] as { name, party } (name)}
						<div>
							<h3 class="font-semibold">{name}</h3>
							<dl class="mt-2 space-y-1 text-sm tabular">
								<div class="flex justify-between gap-4">
									<dt class="text-granite-dark">In wallet</dt>
									<dd class="font-semibold">{money(party.inWallet)}</dd>
								</div>
								<div class="flex justify-between gap-4">
									<dt class="text-granite-dark">Still in the vault</dt>
									<dd class="font-semibold">{money(party.inVault)}</dd>
								</div>
								<div class="flex justify-between gap-4">
									<dt class="text-granite-dark">Withdrawable now</dt>
									<dd class="font-semibold">{money(party.withdrawableNow)}</dd>
								</div>
							</dl>
						</div>
					{/each}
				</div>

				<h3 class="mt-8 font-semibold">Timeline</h3>
				<ol class="mt-2 space-y-3 text-sm tabular">
					{#each timeline as row (row.label)}
						<li>
							<div class="flex justify-between gap-4">
								<span>{row.label}</span>
								<span class="text-granite-dark"
									>{row.block === null ? 'Not yet' : `Block ${row.block}`}</span
								>
							</div>
							<div class="relative mx-1.5 mt-1 h-2 rounded-full bg-line" aria-hidden="true">
								{#if row.block !== null}
									<span
										class="absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full bg-fjord"
										style="left: {pct(row.block)}%"
									></span>
								{/if}
							</div>
						</li>
					{/each}
				</ol>
				{#if caption}
					<p class="mt-3">{caption}</p>
				{/if}
			{/if}
		</div>
	</section>
{/if}
