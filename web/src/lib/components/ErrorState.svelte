<script lang="ts">
	import Icon from './Icon.svelte';

	let {
		title = 'Something did not load',
		message,
		onretry,
		retrying = false
	}: { title?: string; message: string; onretry?: () => void; retrying?: boolean } = $props();
</script>

<div class="max-w-xl border-l-4 border-ink bg-white px-4 py-4" role="alert">
	<p class="flex items-center gap-2 font-semibold"><Icon name="alert" />{title}</p>
	<p class="mt-1 text-granite-dark">{message}</p>
	{#if onretry}
		<button
			type="button"
			class="mt-3 rounded-md bg-fjord px-4 py-2 font-medium text-white hover:bg-fjord-dark disabled:opacity-60"
			disabled={retrying}
			onclick={onretry}
		>
			{retrying ? 'Trying again' : 'Try again'}
		</button>
	{/if}
</div>
