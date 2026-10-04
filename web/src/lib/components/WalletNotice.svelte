<script lang="ts">
	import { wallet } from '$lib/wallet.svelte';
	import Icon from './Icon.svelte';

	// Explains why wallet actions are unavailable and offers the fix (docs/UI_UX.md section 5.5 and 5.6).
	let { class: cls = '' }: { class?: string } = $props();
</script>

{#if wallet.issue}
	<div class="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-ink {cls}" role="status">
		<p class="flex min-w-0 flex-1 basis-56 items-start gap-2">
			<span class="mt-0.5"><Icon name="info" /></span>
			<span>{wallet.issue.message}</span>
		</p>
		{#if wallet.issue.kind === 'wrong_network'}
			<button
				type="button"
				class="btn btn-sm btn-primary"
				disabled={wallet.switching}
				onclick={() => wallet.switchNetwork()}
			>
				Switch network
			</button>
		{:else if wallet.issue.kind === 'not_connected'}
			<button
				type="button"
				class="btn btn-sm btn-primary"
				disabled={wallet.connecting}
				onclick={() => wallet.connect()}
			>
				Connect wallet
			</button>
		{/if}
	</div>
	{#if wallet.notice}
		<p class="mt-2 text-sm text-ink" role="status">{wallet.notice}</p>
	{/if}
{/if}
