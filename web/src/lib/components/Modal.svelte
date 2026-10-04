<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	let {
		title,
		onclose,
		locked = false,
		children,
		footer
	}: {
		title: string;
		onclose: () => void;
		/** When true, Esc, the backdrop and the close button do nothing (a wallet step is waiting). */
		locked?: boolean;
		children: Snippet;
		footer?: Snippet;
	} = $props();

	let dialog = $state<HTMLDialogElement | null>(null);
	const uid = $props.id();

	// showModal() gives a real focus trap, inert background and Esc handling.
	$effect(() => {
		if (dialog && !dialog.open) dialog.showModal();
	});

	// Give focus back to whatever opened the dialog once it closes.
	$effect(() => {
		const opener = document.activeElement as HTMLElement | null;
		return () => opener?.focus?.();
	});

	function onCancel(e: Event) {
		e.preventDefault();
		if (!locked) onclose();
	}

	function onBackdrop(e: MouseEvent) {
		if (e.target === dialog && !locked) onclose();
	}
</script>

<dialog
	bind:this={dialog}
	aria-labelledby="{uid}-title"
	oncancel={onCancel}
	onclick={onBackdrop}
	class="fixed inset-0 m-0 h-dvh max-h-none w-full max-w-none bg-transparent p-0 backdrop:bg-ink/40 sm:left-auto sm:w-xl sm:max-w-xl"
>
	<div
		class="flex h-full flex-col bg-white sm:border-l sm:border-line sm:shadow-[0_24px_48px_-16px_rgba(21,35,45,0.35)]"
	>
		<div class="flex items-start justify-between gap-4 border-b border-line px-6 py-5 sm:px-7">
			<h2 id="{uid}-title" class="text-xl font-semibold">{title}</h2>
			<button
				type="button"
				class="-mt-1 -mr-2 rounded-[10px] p-2 text-granite-dark hover:bg-well hover:text-ink disabled:opacity-40"
				aria-label="Close"
				disabled={locked}
				onclick={onclose}
			>
				<Icon name="close" size={20} />
			</button>
		</div>
		<div class="min-h-0 flex-1 overflow-y-auto p-6 sm:p-7">
			{@render children()}
		</div>
		{#if footer}
			<div class="border-t border-line bg-white px-6 py-5 sm:px-7">
				{@render footer()}
			</div>
		{/if}
	</div>
</dialog>
