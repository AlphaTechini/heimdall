<script lang="ts">
	import Icon from './Icon.svelte';

	let { label, text }: { label: string; text: string } = $props();
	let open = $state(false);
	let hover = $state(false);
	const id = $props.id();
</script>

<span class="relative inline-flex">
	<button
		type="button"
		class="inline-flex size-6 items-center justify-center rounded-full text-granite-dark hover:text-ink"
		aria-label={label}
		aria-expanded={open}
		aria-describedby="{id}-tip"
		onclick={() => (open = !open)}
		onmouseenter={() => (hover = true)}
		onmouseleave={() => (hover = false)}
		onblur={() => (open = false)}
		onkeydown={(e) => e.key === 'Escape' && (open = false)}
	>
		<Icon name="info" size={16} />
	</button>
	<span
		id="{id}-tip"
		role="tooltip"
		class="absolute top-full left-0 z-20 mt-1 w-64 rounded-md bg-ink px-3 py-2 text-sm font-normal text-white {open ||
		hover
			? 'block'
			: 'hidden'}"
	>
		{text}
	</span>
</span>
