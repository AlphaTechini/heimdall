<script lang="ts">
	import Icon from './Icon.svelte';

	let {
		title = 'Something did not load',
		message,
		onretry,
		retrying = false
	}: { title?: string; message: string; onretry?: () => void; retrying?: boolean } = $props();
</script>

<div class="max-w-xl rounded-[14px] border border-line bg-well px-5 py-5" role="alert">
	<p class="flex items-center gap-2 font-semibold"><Icon name="alert" />{title}</p>
	<p class="mt-1 text-granite-dark">{message}</p>
	{#if onretry}
		<button type="button" class="mt-4 btn btn-sm btn-primary" disabled={retrying} onclick={onretry}>
			{retrying ? 'Trying again' : 'Try again'}
		</button>
	{/if}
</div>
