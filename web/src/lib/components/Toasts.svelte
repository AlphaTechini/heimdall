<script lang="ts">
	import { toasts } from '$lib/toast.svelte';
	import Icon from './Icon.svelte';
</script>

<div
	class="pointer-events-none fixed inset-x-4 bottom-4 z-50 flex flex-col items-start gap-2 sm:right-auto sm:left-6"
	role="status"
	aria-live="polite"
>
	{#each toasts.items as toast (toast.id)}
		<div
			class="pointer-events-auto flex max-w-sm items-start gap-3 rounded-xl bg-ink px-4 py-3 text-white"
		>
			<span class="mt-0.5"><Icon name="check" /></span>
			<p class="min-w-0 flex-1">{toast.message}</p>
			<button
				type="button"
				class="-mr-1 rounded p-1 text-white/80 hover:text-white focus-visible:outline-white"
				aria-label="Dismiss message"
				onclick={() => toasts.dismiss(toast.id)}
			>
				<Icon name="close" size={14} />
			</button>
		</div>
	{/each}
</div>
