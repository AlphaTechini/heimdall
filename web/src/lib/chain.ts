import {
	createPublicClient,
	createWalletClient,
	custom,
	defineChain,
	erc20Abi,
	keccak256,
	maxUint256,
	parseEventLogs,
	toBytes,
	zeroAddress,
	type Hash
} from 'viem';
import { heimdallGuardAbi } from './abi/HeimdallGuard';
import { heimdallGuardFactoryAbi } from './abi/HeimdallGuardFactory';
import { app } from './app.svelte';
import { chainSpec } from './chains';
import { AppError } from './errors';
import type { ConfigTarget, TargetType } from './types';
import { wallet } from './wallet.svelte';

type Address = `0x${string}`;

/** HeimdallGuard.PositionType: ERC4626 = 0, AAVE_V3 = 1. */
export function positionType(type: TargetType): 0 | 1 {
	return type === 'ERC4626' ? 0 : 1;
}

/** The reason recorded on-chain for an exit the owner starts (docs: Exit now). */
export const MANUAL_EXIT_REASON = 'Manual exit by owner';

function clients() {
	const account = wallet.assertReady();
	const provider = wallet.provider;
	const cfg = app.config;
	if (!provider || !cfg) throw new AppError('no_wallet', 'No wallet found in this browser.');
	const spec = chainSpec(cfg.chainId, cfg.chainName);
	const chain = defineChain({
		id: cfg.chainId,
		name: spec.name,
		nativeCurrency: { name: 'Ether', symbol: 'ETH', decimals: 18 },
		rpcUrls: { default: { http: [spec.rpcUrl || 'http://127.0.0.1:8545'] } }
	});
	const transport = custom(provider);
	return {
		account,
		chain,
		factory: cfg.factory,
		publicClient: createPublicClient({ chain, transport }),
		walletClient: createWalletClient({ account, chain, transport })
	};
}

export type OnSubmitted = (hash: Hash) => void;

async function confirm(publicClient: ReturnType<typeof clients>['publicClient'], hash: Hash) {
	const receipt = await publicClient.waitForTransactionReceipt({ hash });
	if (receipt.status !== 'success') {
		throw new AppError(
			'other',
			'The transaction was mined but failed on-chain. Check your balance and the position, then try again.'
		);
	}
	return receipt;
}

/** The Guard owned by the connected wallet, or null when it has not been created yet. */
export async function guardOfOwner(): Promise<Address | null> {
	const { publicClient, factory, account } = clients();
	const guard = await publicClient.readContract({
		address: factory,
		abi: heimdallGuardFactoryAbi,
		functionName: 'guardOf',
		args: [account]
	});
	return guard === zeroAddress ? null : guard;
}

export async function createGuard(onSubmitted?: OnSubmitted): Promise<Address> {
	const { publicClient, walletClient, factory, account, chain } = clients();
	const hash = await walletClient.writeContract({
		address: factory,
		abi: heimdallGuardFactoryAbi,
		functionName: 'createGuard',
		account,
		chain
	});
	onSubmitted?.(hash);
	const receipt = await confirm(publicClient, hash);
	const created = parseEventLogs({
		abi: heimdallGuardFactoryAbi,
		logs: receipt.logs,
		eventName: 'GuardCreated'
	}).find((l) => l.args.owner.toLowerCase() === account.toLowerCase());
	if (created) return created.args.guard;
	const found = await guardOfOwner();
	if (!found)
		throw new AppError(
			'other',
			'The Guard was created but its address could not be read. Reload the page.'
		);
	return found;
}

export async function positionTokenBalance(token: Address): Promise<bigint> {
	const { publicClient, account } = clients();
	return publicClient.readContract({
		address: token,
		abi: erc20Abi,
		functionName: 'balanceOf',
		args: [account]
	});
}

export async function allowanceFor(token: Address, guard: Address): Promise<bigint> {
	const { publicClient, account } = clients();
	return publicClient.readContract({
		address: token,
		abi: erc20Abi,
		functionName: 'allowance',
		args: [account, guard]
	});
}

export async function approve(
	token: Address,
	guard: Address,
	amount: bigint,
	onSubmitted?: OnSubmitted
) {
	const { publicClient, walletClient, account, chain } = clients();
	const hash = await walletClient.writeContract({
		address: token,
		abi: erc20Abi,
		functionName: 'approve',
		args: [guard, amount],
		account,
		chain
	});
	onSubmitted?.(hash);
	await confirm(publicClient, hash);
	return hash;
}

export async function depositIntoGuard(
	guard: Address,
	target: ConfigTarget,
	amount: bigint,
	onSubmitted?: OnSubmitted
) {
	const { publicClient, walletClient, account, chain } = clients();
	const hash = await walletClient.writeContract({
		address: guard,
		abi: heimdallGuardAbi,
		functionName: 'deposit',
		args: [positionType(target.type), target.address, amount],
		account,
		chain
	});
	onSubmitted?.(hash);
	await confirm(publicClient, hash);
	return hash;
}

export async function setKeeperEnabled(
	guard: Address,
	enabled: boolean,
	onSubmitted?: OnSubmitted
) {
	const { publicClient, walletClient, account, chain } = clients();
	const hash = await walletClient.writeContract({
		address: guard,
		abi: heimdallGuardAbi,
		functionName: 'setKeeperEnabled',
		args: [enabled],
		account,
		chain
	});
	onSubmitted?.(hash);
	await confirm(publicClient, hash);
	return hash;
}

export async function setPaused(guard: Address, paused: boolean, onSubmitted?: OnSubmitted) {
	const { publicClient, walletClient, account, chain } = clients();
	const hash = await walletClient.writeContract({
		address: guard,
		abi: heimdallGuardAbi,
		functionName: 'setPaused',
		args: [paused],
		account,
		chain
	});
	onSubmitted?.(hash);
	await confirm(publicClient, hash);
	return hash;
}

/** Returns everything the Guard holds for this position (position tokens, not redeemed) to the owner. */
export async function withdrawFromGuard(
	guard: Address,
	target: ConfigTarget,
	onSubmitted?: OnSubmitted
) {
	const { publicClient, walletClient, account, chain } = clients();
	// Read what the Guard holds right now, so a stale API number cannot leave dust behind.
	const held = await publicClient.readContract({
		address: guard,
		abi: heimdallGuardAbi,
		functionName: 'held',
		args: [positionType(target.type), target.address]
	});
	if (held === 0n) throw new AppError('other', 'Nothing is left in your Guard for this position.');
	const hash = await walletClient.writeContract({
		address: guard,
		abi: heimdallGuardAbi,
		functionName: 'withdraw',
		args: [positionType(target.type), target.address, held],
		account,
		chain
	});
	onSubmitted?.(hash);
	await confirm(publicClient, hash);
	return hash;
}

/** Owner exit: redeems as much as the vault can pay now, straight to the owner's wallet. */
export async function exitNow(guard: Address, target: ConfigTarget, onSubmitted?: OnSubmitted) {
	const { publicClient, walletClient, account, chain } = clients();
	const hash = await walletClient.writeContract({
		address: guard,
		abi: heimdallGuardAbi,
		functionName: 'exit',
		args: [
			positionType(target.type),
			target.address,
			maxUint256,
			keccak256(toBytes(MANUAL_EXIT_REASON))
		],
		account,
		chain
	});
	onSubmitted?.(hash);
	await confirm(publicClient, hash);
	return hash;
}

/** Reads the Guard's switches straight from the chain so the UI never shows a stale value. */
export async function readGuardSwitches(guard: Address) {
	const { publicClient } = clients();
	const [keeperEnabled, paused] = await Promise.all([
		publicClient.readContract({
			address: guard,
			abi: heimdallGuardAbi,
			functionName: 'keeperEnabled'
		}),
		publicClient.readContract({ address: guard, abi: heimdallGuardAbi, functionName: 'paused' })
	]);
	return { keeperEnabled, paused };
}
