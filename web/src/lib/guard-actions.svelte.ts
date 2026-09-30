import { Action } from './action.svelte';
import { exitNow, setKeeperEnabled, setPaused, withdrawFromGuard } from './chain';
import { AppError } from './errors';
import { positions } from './positions.svelte';
import { toasts } from './toast.svelte';
import type { ConfigTarget } from './types';

function requireGuard(): `0x${string}` {
	const guard = positions.guard;
	if (!guard) throw new AppError('other', 'No Guard found for this wallet. Reload the page.');
	return guard;
}

/** The owner's on-chain actions on their Guard, each with idle / pending / success / error states. */
export function createGuardActions(getTarget: () => ConfigTarget) {
	const toggle = new Action('Could not change protection.', async (ctx) => {
		const guard = requireGuard();
		const next = !positions.keeperEnabled;
		ctx.pending('Waiting for wallet signature');
		await setKeeperEnabled(guard, next, () => ctx.pending('Confirming on Arbitrum'));
		await positions.load();
		ctx.success(
			next
				? 'Protection is on. Heimdall will exit this position if risk becomes Critical.'
				: 'Protection is off. Heimdall will not exit this position until you turn it back on.'
		);
		toasts.show(next ? 'Protection turned on' : 'Protection turned off');
	});

	const withdraw = new Action('Could not withdraw.', async (ctx) => {
		const guard = requireGuard();
		ctx.pending('Waiting for wallet signature');
		await withdrawFromGuard(guard, getTarget(), () => ctx.pending('Confirming on Arbitrum'));
		await positions.load();
		ctx.success('Withdrawn. Your position is back in your wallet and no longer protected.');
		toasts.show('Withdrawn to your wallet');
	});

	const pause = new Action('Could not change the pause.', async (ctx) => {
		const guard = requireGuard();
		const next = !positions.paused;
		ctx.pending('Waiting for wallet signature');
		await setPaused(guard, next, () => ctx.pending('Confirming on Arbitrum'));
		await positions.load();
		ctx.success(
			next
				? 'Heimdall is paused. It will not exit for you until you resume.'
				: 'Heimdall is running again.'
		);
	});

	const exit = new Action('Could not start the exit.', async (ctx) => {
		const guard = requireGuard();
		ctx.pending('Waiting for wallet signature');
		await exitNow(guard, getTarget(), () => ctx.pending('Confirming on Arbitrum'));
		await positions.load();
		ctx.success('Exit sent. Whatever the vault could pay now is in your wallet.');
		toasts.show('Exit complete');
	});

	return {
		toggle,
		withdraw,
		pause,
		exit,
		get busy() {
			return toggle.busy || withdraw.busy || pause.busy || exit.busy;
		}
	};
}
