<script lang="ts">
	// Small inline icons (stroke, currentColor). Purely visual; always paired with text.
	type Name = 'info' | 'check' | 'alert' | 'shield' | 'external' | 'lock' | 'close' | 'chevron';
	let { name, size = 16 }: { name: Name; size?: number } = $props();
</script>

<svg
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width="2"
	stroke-linecap="round"
	stroke-linejoin="round"
	aria-hidden="true"
	focusable="false"
	class="shrink-0"
>
	{#if name === 'info'}
		<circle cx="12" cy="12" r="9" /><path d="M12 11v5" /><path d="M12 8h.01" />
	{:else if name === 'check'}
		<path d="M5 12.5l4.5 4.5L19 7.5" />
	{:else if name === 'alert'}
		<path d="M12 3l10 18H2L12 3z" /><path d="M12 10v5" /><path d="M12 18h.01" />
	{:else if name === 'shield'}
		<path d="M12 3l8 3v6c0 4.5-3.2 8-8 9-4.8-1-8-4.5-8-9V6l8-3z" /><path
			d="M8.5 12l2.5 2.5 4.5-5"
		/>
	{:else if name === 'external'}
		<path d="M14 4h6v6" /><path d="M20 4l-9 9" /><path
			d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"
		/>
	{:else if name === 'lock'}
		<rect x="5" y="11" width="14" height="9" rx="1.5" /><path d="M8 11V8a4 4 0 0 1 8 0v3" />
	{:else if name === 'close'}
		<path d="M6 6l12 12" /><path d="M18 6L6 18" />
	{:else if name === 'chevron'}
		<path d="M6 9l6 6 6-6" />
	{/if}
</svg>
