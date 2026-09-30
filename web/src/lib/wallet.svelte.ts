import { getAddress } from 'viem';
import { app } from './app.svelte';
import { AppError } from './errors';
import { chainLabel, chainSpec } from './chains';

export interface Eip1193Provider {
	request(args: { method: string; params?: unknown[] | object }): Promise<unknown>;
	on?(event: string, listener: (...args: unknown[]) => void): void;
	removeListener?(event: string, listener: (...args: unknown[]) => void): void;
}

export type WalletIssue =
	| { kind: 'no_wallet'; message: string }
	| { kind: 'not_connected'; message: string }
	| { kind: 'wrong_network'; message: string };

const DISCONNECT_KEY = 'heimdall.disconnected';

function storageGet(key: string): string | null {
	try {
		return localStorage.getItem(key);
	} catch {
		return null;
	}
}
function storageSet(key: string, value: string | null) {
	try {
		if (value === null) localStorage.removeItem(key);
		else localStorage.setItem(key, value);
	} catch {
		/* storage can be blocked; the wallet still works for this visit */
	}
}

/** The injected EIP-1193 wallet (MetaMask, Rabby): address, chain and the actions around them. */
class WalletStore {
	provider = $state.raw<Eip1193Provider | null>(null);
	address = $state<`0x${string}` | null>(null);
	chainId = $state<number | null>(null);
	connecting = $state(false);
	switching = $state(false);
	/** True until the first silent check for an already-connected account has finished. */
	restoring = $state(true);
	/** Message from the last connect or switch attempt that failed. */
	notice = $state('');

	installed = $derived(this.provider !== null);
	connected = $derived(this.address !== null);

	expectedChainId = $derived(app.config?.chainId ?? null);
	// The name the server shows in the header, so the message and the header agree.
	expectedChainName = $derived(app.config?.chainName ?? '');
	wrongNetwork = $derived(
		this.connected && this.expectedChainId !== null && this.chainId !== this.expectedChainId
	);

	/** Why wallet actions cannot run right now, or null when they can. */
	issue = $derived.by((): WalletIssue | null => {
		if (!this.installed) {
			return {
				kind: 'no_wallet',
				message:
					'No wallet found in this browser. Install MetaMask or Rabby, then reload this page.'
			};
		}
		if (!this.address) {
			return { kind: 'not_connected', message: 'Connect your wallet to continue.' };
		}
		if (this.wrongNetwork) {
			return {
				kind: 'wrong_network',
				message: `Your wallet is on ${chainLabel(this.chainId)}. Switch to ${this.expectedChainName} to continue.`
			};
		}
		return null;
	});

	private started = false;

	start() {
		if (this.started || typeof window === 'undefined') return;
		this.started = true;
		const provider = window.ethereum ?? null;
		this.provider = provider;
		if (!provider) {
			this.restoring = false;
			return;
		}

		provider.on?.('accountsChanged', (accounts) => this.setAccounts(accounts as string[]));
		provider.on?.('chainChanged', (id) => (this.chainId = Number(id)));

		const accounts =
			storageGet(DISCONNECT_KEY) === '1'
				? Promise.resolve()
				: // Silent: eth_accounts never opens a wallet prompt.
					provider
						.request({ method: 'eth_accounts' })
						.then((a) => this.setAccounts(a as string[]))
						.catch(() => {});
		void Promise.allSettled([this.readChain(), accounts]).then(() => (this.restoring = false));
	}

	private setAccounts(accounts: string[]) {
		this.address = accounts && accounts.length > 0 ? getAddress(accounts[0]) : null;
		if (this.address) storageSet(DISCONNECT_KEY, null);
	}

	private async readChain() {
		try {
			const id = await this.provider?.request({ method: 'eth_chainId' });
			this.chainId = Number(id);
		} catch {
			this.chainId = null;
		}
	}

	async connect() {
		if (!this.provider) {
			this.notice =
				'No wallet found in this browser. Install MetaMask or Rabby, then reload this page.';
			return;
		}
		if (this.connecting) return;
		this.connecting = true;
		this.notice = '';
		try {
			const accounts = (await this.provider.request({ method: 'eth_requestAccounts' })) as string[];
			this.setAccounts(accounts);
			await this.readChain();
		} catch (e) {
			const code = (e as { code?: number }).code;
			this.notice =
				code === 4001
					? 'You closed the connection request. Click Connect wallet when you are ready.'
					: 'Could not connect to your wallet. Unlock it, then try again.';
		} finally {
			this.connecting = false;
		}
	}

	/** Injected wallets cannot be disconnected from a page, so Heimdall just forgets the address. */
	disconnect() {
		this.address = null;
		this.notice = '';
		storageSet(DISCONNECT_KEY, '1');
	}

	async switchNetwork() {
		const cfg = app.config;
		if (!this.provider || !cfg) return;
		if (this.switching) return;
		this.switching = true;
		this.notice = '';
		const spec = chainSpec(cfg.chainId, cfg.chainName);
		const chainIdHex = `0x${cfg.chainId.toString(16)}`;
		try {
			try {
				await this.provider.request({
					method: 'wallet_switchEthereumChain',
					params: [{ chainId: chainIdHex }]
				});
			} catch (e) {
				if ((e as { code?: number }).code !== 4902) throw e;
				if (!spec.rpcUrl) {
					throw new AppError(
						'wrong_network',
						`Your wallet does not know ${spec.name}. Add it in your wallet's network settings, then switch.`
					);
				}
				await this.provider.request({
					method: 'wallet_addEthereumChain',
					params: [
						{
							chainId: chainIdHex,
							chainName: spec.name,
							nativeCurrency: { name: 'Ether', symbol: 'ETH', decimals: 18 },
							rpcUrls: [spec.rpcUrl],
							...(spec.explorerUrl ? { blockExplorerUrls: [spec.explorerUrl] } : {})
						}
					]
				});
			}
			await this.readChain();
		} catch (e) {
			const code = (e as { code?: number }).code;
			this.notice =
				e instanceof AppError
					? e.message
					: code === 4001
						? `You closed the network request. Click Switch network to try again.`
						: `Could not switch to ${spec.name}. Switch networks in your wallet, then try again.`;
		} finally {
			this.switching = false;
		}
	}

	/** Throws an AppError with a fix-it message unless wallet actions can run. */
	assertReady(): `0x${string}` {
		const issue = this.issue;
		if (issue) throw new AppError(issue.kind, issue.message);
		return this.address as `0x${string}`;
	}
}

export const wallet = new WalletStore();
