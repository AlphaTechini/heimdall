<script lang="ts">
	/* eslint-disable no-useless-assignment -- `valid` is a $bindable output that the parent reads */
	import {
		CRITICAL_OPTIONS,
		MAX_TIP_CAP_USD,
		TIP_TOOLTIP,
		WARNING_OPTIONS,
		parseTipCap
	} from '$lib/policy';
	import type { Policy } from '$lib/types';
	import { shortAddress } from '$lib/format';
	import Icon from './Icon.svelte';
	import InfoTip from './InfoTip.svelte';

	let {
		policy = $bindable(),
		valid = $bindable(true),
		safeAddress,
		disabled = false
	}: {
		policy: Policy;
		valid?: boolean;
		safeAddress: string | null;
		disabled?: boolean;
	} = $props();

	const uid = $props.id();
	// The text the user is typing is kept separately so a half-typed value never gets lost.
	let tipText = $state(String(policy.tipCapUsd));
	let touched = $state(false);
	const parsed = $derived(parseTipCap(tipText));
	const showError = $derived(policy.priorityExit && touched && parsed.error !== '');

	$effect(() => {
		valid = !policy.priorityExit || parsed.value !== null;
	});

	function onTipInput(e: Event) {
		tipText = (e.currentTarget as HTMLInputElement).value;
		touched = true;
		const p = parseTipCap(tipText);
		if (p.value !== null) policy.tipCapUsd = p.value;
	}
</script>

<div class="space-y-6">
	<fieldset {disabled} class="space-y-2">
		<legend class="mb-1 font-semibold">When risk is Critical</legend>
		{#each CRITICAL_OPTIONS as opt (opt.value)}
			<label class="flex items-start gap-3 py-1">
				<input
					type="radio"
					class="mt-1"
					name="{uid}-critical"
					value={opt.value}
					bind:group={policy.onCritical}
				/>
				<span>
					<span class="block">{opt.label}</span>
					<span class="block text-sm text-granite-dark">{opt.help}</span>
				</span>
			</label>
		{/each}
	</fieldset>

	<fieldset {disabled} class="space-y-2">
		<legend class="mb-1 font-semibold">When risk is Warning</legend>
		{#each WARNING_OPTIONS as opt (opt.value)}
			<label class="flex items-center gap-3 py-1">
				<input type="radio" name="{uid}-warning" value={opt.value} bind:group={policy.onWarning} />
				<span>{opt.label}</span>
			</label>
		{/each}
	</fieldset>

	<div>
		<p class="font-semibold">Send my money to</p>
		<p class="mt-1">My wallet, as USDC</p>
	</div>

	<div>
		<div class="flex items-center gap-1">
			<span class="font-semibold" id="{uid}-speed">Speed</span>
			<InfoTip label="What is a priority exit?" text={TIP_TOOLTIP} />
		</div>
		<div class="mt-1 flex items-center gap-3">
			<button
				type="button"
				role="switch"
				aria-checked={policy.priorityExit}
				aria-labelledby="{uid}-priority"
				{disabled}
				class="relative inline-flex h-7 w-12 shrink-0 items-center rounded-full border border-granite transition-colors disabled:opacity-60 {policy.priorityExit
					? 'bg-fjord'
					: 'bg-white'}"
				onclick={() => (policy.priorityExit = !policy.priorityExit)}
			>
				<span
					class="inline-block size-5 rounded-full transition-transform {policy.priorityExit
						? 'translate-x-6 bg-white'
						: 'translate-x-1 bg-granite-dark'}"
				></span>
			</button>
			<span id="{uid}-priority">Priority exit</span>
		</div>

		<div class="mt-3 max-w-xs">
			<label for="{uid}-tip" class="block text-sm font-medium">Pay up to (US dollars)</label>
			<div class="relative mt-1">
				<span
					class="pointer-events-none absolute inset-y-0 left-3 flex items-center text-granite-dark"
					>$</span
				>
				<input
					id="{uid}-tip"
					type="text"
					inputmode="decimal"
					autocomplete="off"
					class="w-full rounded-md py-2 pr-3 pl-7 tabular disabled:bg-frost disabled:text-granite-dark {showError
						? 'border-2 border-ink'
						: 'border-granite'}"
					value={tipText}
					disabled={disabled || !policy.priorityExit}
					aria-invalid={showError}
					aria-describedby="{uid}-tip-help"
					oninput={onTipInput}
					onblur={() => (touched = true)}
				/>
			</div>
			<p id="{uid}-tip-help" class="mt-1 text-sm {showError ? 'text-ink' : 'text-granite-dark'}">
				{#if !policy.priorityExit}
					Priority exit is off, so Heimdall pays no extra. Turn it on to set a limit.
				{:else if showError}
					<span class="inline-flex items-start gap-1.5" role="alert">
						<span class="mt-0.5"><Icon name="alert" size={14} /></span>
						{parsed.error}
					</span>
				{:else}
					Heimdall never pays more than this in extra fees on one exit. The most you can set is ${MAX_TIP_CAP_USD}.
				{/if}
			</p>
		</div>
	</div>

	<div>
		<p class="font-semibold">Safe address</p>
		<p class="mt-1 flex items-center gap-2 break-all tabular">
			<Icon name="lock" />
			{safeAddress ?? 'Connect your wallet to see it'}
		</p>
		<p class="mt-1 text-sm text-granite-dark">
			{#if safeAddress}
				This is your own wallet ({shortAddress(safeAddress)}). It is locked: Heimdall can only send
				money here.
			{:else}
				Money only ever goes back to the wallet you connect. It cannot be changed to another
				address.
			{/if}
		</p>
	</div>
</div>
