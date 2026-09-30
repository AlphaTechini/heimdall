<script lang="ts">
	import type { Described } from '$lib/errors';
	import { wallet } from '$lib/wallet.svelte';
	import Icon from './Icon.svelte';

	interface ActionLike {
		state: 'idle' | 'pending' | 'success' | 'error';
		pendingLabel: string;
		message: string;
		error: Described | null;
	}

	let { action, class: cls = '' }: { action: ActionLike; class?: string } = $props();
</script>

<!-- One live region per action so screen readers hear pending, success and error changes. -->
<div aria-live="polite" class="text-sm {cls}">
	{#if action.state === 'pending'}
		<p class="flex items-start gap-2 text-ink">
			<span class="mt-1.5 size-2 shrink-0 rounded-full bg-fjord" aria-hidden="true"></span>
			{action.pendingLabel}
		</p>
	{:else if action.state === 'success' && action.message}
		<p class="flex items-start gap-2 text-ink">
			<span class="mt-0.5 text-fjord"><Icon name="check" /></span>
			{action.message}
		</p>
	{:else if action.state === 'error' && action.error}
		<div class="flex flex-wrap items-start gap-x-3 gap-y-2 text-ink">
			<p class="flex min-w-0 flex-1 basis-56 items-start gap-2">
				<span class="mt-0.5"><Icon name="alert" /></span>
				<span>{action.error.message}</span>
			</p>
			{#if action.error.kind === 'wrong_network'}
				<button
					type="button"
					class="rounded-md border border-fjord px-3 py-1.5 font-medium text-fjord hover:bg-white disabled:opacity-60"
					disabled={wallet.switching}
					onclick={() => wallet.switchNetwork()}
				>
					Switch network
				</button>
			{:else if action.error.kind === 'not_connected'}
				<button
					type="button"
					class="rounded-md border border-fjord px-3 py-1.5 font-medium text-fjord hover:bg-white disabled:opacity-60"
					disabled={wallet.connecting}
					onclick={() => wallet.connect()}
				>
					Connect wallet
				</button>
			{/if}
		</div>
	{/if}
</div>
