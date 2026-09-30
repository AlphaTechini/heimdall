<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '$lib/api';
	import { app } from '$lib/app.svelte';
	import { auth } from '$lib/auth.svelte';
	import {
		allowanceFor,
		approve,
		createGuard,
		depositIntoGuard,
		guardOfOwner,
		positionTokenBalance
	} from '$lib/chain';
	import { AppError, describeError, type Described } from '$lib/errors';
	import { formatAmount } from '$lib/format';
	import { txLink } from '$lib/explorer';
	import { defaultPolicy } from '$lib/policy';
	import { positions } from '$lib/positions.svelte';
	import { toasts } from '$lib/toast.svelte';
	import type { ConfigTarget, Policy } from '$lib/types';
	import { wallet } from '$lib/wallet.svelte';
	import ExplorerLink from './ExplorerLink.svelte';
	import Icon from './Icon.svelte';
	import Modal from './Modal.svelte';
	import PolicyForm from './PolicyForm.svelte';
	import WalletNotice from './WalletNotice.svelte';

	let { target, onclose }: { target: ConfigTarget; onclose: () => void } = $props();

	type StepState = 'idle' | 'pending' | 'success' | 'error';
	interface Step {
		title: string;
		idle: string;
		state: StepState;
		detail: string;
		hash: string | null;
		error: Described | null;
	}

	let policy = $state<Policy>(defaultPolicy(app.config?.defaultTipCapUsd ?? 2));
	let valid = $state(true);
	let running = $state(false);
	let guardAddress = $state<`0x${string}` | null>(positions.guard);

	let steps = $state<Step[]>([
		{
			title: 'Create your Guard',
			idle: 'Your personal Guard contract. Only you own it.',
			state: positions.guard ? 'success' : 'idle',
			detail: positions.guard ? 'Your Guard already exists.' : '',
			hash: null,
			error: null
		},
		{
			title: 'Move your position into it',
			idle: 'Approve, then deposit your position. You can withdraw it any time.',
			state: 'idle',
			detail: '',
			hash: null,
			error: null
		},
		{
			title: 'Sign in and save your policy',
			idle: 'One signature proves the wallet is yours. It costs no gas.',
			state: 'idle',
			detail: '',
			hash: null,
			error: null
		}
	]);

	const position = $derived(positions.position(target.id));
	const finished = $derived(steps.every((s) => s.state === 'success'));
	const pendingStep = $derived(steps.find((s) => s.state === 'pending'));

	onMount(() => {
		// A signed-in user has a default policy saved in Settings: start from it.
		if (auth.token) {
			api
				.settings(auth.token)
				.then((s) => (policy = { ...s.defaultPolicy }))
				.catch(() => {});
		}
	});

	function setStep(i: number, patch: Partial<Step>) {
		steps[i] = { ...steps[i], ...patch };
	}

	async function stepCreateGuard() {
		const existing = await guardOfOwner();
		if (existing) {
			guardAddress = existing;
			setStep(0, { detail: 'Your Guard already exists.' });
			return;
		}
		setStep(0, { detail: 'Waiting for wallet signature' });
		guardAddress = await createGuard((hash) =>
			setStep(0, { hash, detail: 'Confirming on Arbitrum' })
		);
		setStep(0, { detail: 'Guard created.' });
	}

	async function stepMove() {
		const guard = guardAddress;
		if (!guard) throw new AppError('other', 'Create your Guard first.');
		setStep(1, { detail: 'Reading your balance' });
		const balance = await positionTokenBalance(target.positionToken);
		if (balance === 0n) {
			throw new AppError(
				'other',
				`Your wallet holds none of ${target.label}. Deposit into it first, or connect the wallet that holds it.`
			);
		}
		const allowance = await allowanceFor(target.positionToken, guard);
		if (allowance < balance) {
			setStep(1, { detail: 'Waiting for wallet signature: approve the move (step 1 of 2)' });
			await approve(target.positionToken, guard, balance, (hash) =>
				setStep(1, { hash, detail: 'Confirming the approval on Arbitrum' })
			);
		}
		setStep(1, {
			hash: null,
			detail: 'Waiting for wallet signature: move your position (step 2 of 2)'
		});
		await depositIntoGuard(guard, target, balance, (hash) =>
			setStep(1, { hash, detail: 'Confirming the move on Arbitrum' })
		);
		setStep(1, { detail: 'Position moved into your Guard.' });
	}

	async function stepSave() {
		const guard = guardAddress;
		if (!guard) throw new AppError('other', 'Create your Guard first.');
		if (!auth.token) setStep(2, { detail: 'Waiting for wallet signature' });
		await auth.ensure();
		setStep(2, { detail: 'Saving your policy' });
		await auth.call((token) => api.putPolicy(token, guard, target.id, policy));
		setStep(2, { detail: 'Policy saved.' });
	}

	const runners = [stepCreateGuard, stepMove, stepSave];

	/** Runs every step that is not done yet, in order. Stops at the first error; Retry resumes there. */
	async function activate() {
		if (running) return;
		running = true;
		try {
			for (let i = 0; i < runners.length; i++) {
				if (steps[i].state === 'success') continue;
				setStep(i, { state: 'pending', error: null, hash: null });
				try {
					wallet.assertReady();
					await runners[i]();
					setStep(i, { state: 'success' });
					if (i === 0) void positions.load();
				} catch (e) {
					setStep(i, {
						state: 'error',
						detail: '',
						error: describeError(e, 'This step did not finish.')
					});
					return;
				}
			}
			toasts.show('Protection active');
			void positions.load();
			onclose();
		} finally {
			running = false;
		}
	}

	const blocker = $derived(
		wallet.issue ? null : !valid ? 'Fix the priority limit above before you continue.' : null
	);
</script>

<Modal title="Turn on protection" {onclose} locked={running}>
	<p class="text-lg font-semibold">{target.label}</p>
	{#if position}
		<p class="text-granite-dark tabular">
			{formatAmount(position.walletAmount, target.assetDecimals)}
			{target.assetSymbol} in your wallet
		</p>
	{/if}
	<p class="mt-3 flex items-start gap-2 rounded-md bg-frost px-3 py-2 text-sm">
		<span class="mt-0.5 text-fjord"><Icon name="shield" /></span>
		<span
			>Heimdall can only send this money back to you. You can withdraw or turn this off any time.</span
		>
	</p>

	<div class="mt-6">
		<PolicyForm bind:policy bind:valid safeAddress={wallet.address} disabled={running} />
	</div>

	<h3 class="mt-8 text-xl display">Setup steps</h3>
	<ol class="mt-3 space-y-3" aria-live="polite">
		{#each steps as step, i (step.title)}
			<li class="flex gap-3">
				<span
					class="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full border text-sm font-semibold {step.state ===
					'success'
						? 'border-fjord bg-fjord text-white'
						: 'border-granite text-granite-dark'}"
					aria-hidden="true"
				>
					{#if step.state === 'success'}<Icon name="check" size={14} />{:else}{i + 1}{/if}
				</span>
				<div class="min-w-0 flex-1">
					<p class="font-medium">
						{step.title}
						<span class="sr-only">
							{step.state === 'success'
								? 'Done'
								: step.state === 'pending'
									? 'In progress'
									: step.state === 'error'
										? 'Needs attention'
										: 'Not started'}
						</span>
					</p>
					{#if step.state === 'idle'}
						<p class="text-sm text-granite-dark">{step.idle}</p>
					{:else if step.state === 'pending'}
						<p class="text-sm">{step.detail}</p>
					{:else if step.state === 'success'}
						<p class="text-sm text-granite-dark">
							{step.detail || 'Done.'}
							{#if step.hash}
								<ExplorerLink link={txLink(step.hash)}>View transaction</ExplorerLink>
							{/if}
						</p>
					{:else if step.error}
						<p class="text-sm" role="alert">{step.error.message}</p>
						<div class="mt-2 flex flex-wrap gap-2">
							{#if step.error.kind === 'wrong_network'}
								<button
									type="button"
									class="rounded-md border border-fjord px-3 py-1.5 text-sm font-medium text-fjord hover:bg-frost"
									onclick={() => wallet.switchNetwork()}
								>
									Switch network
								</button>
							{/if}
							<button
								type="button"
								class="rounded-md border border-fjord px-3 py-1.5 text-sm font-medium text-fjord hover:bg-frost disabled:opacity-60"
								disabled={running}
								onclick={activate}
							>
								Retry
							</button>
						</div>
					{/if}
				</div>
			</li>
		{/each}
	</ol>

	{#snippet footer()}
		{#if wallet.issue}
			<WalletNotice class="mb-3" />
		{:else if blocker}
			<p class="mb-3 text-sm">{blocker}</p>
		{/if}
		{#if running && pendingStep}
			<p class="mb-3 text-sm text-granite-dark">
				A step is waiting. Approve or reject it in your wallet. You can cancel after that.
			</p>
		{/if}
		<div class="flex flex-wrap gap-3">
			<button
				type="button"
				class="rounded-md bg-fjord px-5 py-2.5 font-medium text-white hover:bg-fjord-dark disabled:opacity-60"
				disabled={running || finished || !!wallet.issue || !valid}
				onclick={activate}
			>
				Activate protection
			</button>
			<button
				type="button"
				class="rounded-md border border-granite px-5 py-2.5 font-medium hover:bg-frost disabled:opacity-60"
				disabled={running}
				onclick={onclose}
			>
				Cancel
			</button>
		</div>
	{/snippet}
</Modal>
