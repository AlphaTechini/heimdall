<script lang="ts">
	import { resolve } from '$app/paths';
	import { api } from '$lib/api';
	import { auth } from '$lib/auth.svelte';
	import { wallet } from '$lib/wallet.svelte';
	import Icon from './Icon.svelte';

	// Shown right after a wallet connects, until alerts are set up or the person dismisses it.
	// Signed-in wallets are checked silently; others see it without being asked to sign.
	const KEY = 'heimdall.alertsPrompt.dismissed';
	const uid = $props.id();

	function dismissedFor(address: string): boolean {
		try {
			return (localStorage.getItem(KEY) ?? '').split(',').includes(address.toLowerCase());
		} catch {
			return false;
		}
	}

	let dismissed = $state(false);
	let alertsOn = $state(false);

	$effect(() => {
		const address = wallet.address;
		dismissed = address ? dismissedFor(address) : true;
		alertsOn = false;
		const token = auth.token;
		if (!address || !token || dismissed) return;
		api
			.settings(token)
			.then((s) => {
				if (wallet.address === address) {
					alertsOn = s.telegram?.linked === true || s.email?.verified === true;
				}
			})
			.catch(() => {});
	});

	function dismiss() {
		const address = wallet.address;
		dismissed = true;
		if (!address) return;
		try {
			const list = (localStorage.getItem(KEY) ?? '').split(',').filter(Boolean);
			list.push(address.toLowerCase());
			localStorage.setItem(KEY, list.join(','));
		} catch {
			/* stays dismissed for this visit */
		}
	}
</script>

{#if wallet.connected && !dismissed && !alertsOn}
	<section aria-labelledby="{uid}-title" class="panel px-6 py-5 sm:px-7">
		<div class="flex items-start gap-3">
			<span class="mt-0.5 text-fjord"><Icon name="bell" size={22} /></span>
			<div class="min-w-0">
				<h2 id="{uid}-title" class="text-lg font-semibold">Get told the moment Heimdall acts</h2>
				<p class="mt-1 max-w-[60ch] text-granite-dark">
					Heimdall exits on its own when a vault turns critical. Alerts reach your phone or inbox
					when risk rises, when an exit starts, and when your money is back in your wallet.
				</p>
				<div class="mt-4 flex flex-wrap items-center gap-3">
					<a href="{resolve('/settings')}#telegram" class="btn btn-primary">Connect Telegram</a>
					<a href="{resolve('/settings')}#email" class="btn btn-secondary">Add email</a>
					<button
						type="button"
						class="rounded-md px-2 py-1 text-sm font-medium text-granite-dark hover:text-ink"
						onclick={dismiss}
					>
						Not now
					</button>
				</div>
			</div>
		</div>
	</section>
{/if}
