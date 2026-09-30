import { createWalletClient, custom } from 'viem';
import { api, ApiError } from './api';
import { AppError } from './errors';
import { wallet } from './wallet.svelte';

const KEY = 'heimdall.session';

interface Session {
	token: string;
	address: string;
	expiresAt: string;
}

function load(): Session | null {
	try {
		const raw = sessionStorage.getItem(KEY);
		return raw ? (JSON.parse(raw) as Session) : null;
	} catch {
		return null;
	}
}
function save(session: Session | null) {
	try {
		if (session) sessionStorage.setItem(KEY, JSON.stringify(session));
		else sessionStorage.removeItem(KEY);
	} catch {
		/* the token still lives in memory */
	}
}

/** Sign-in with Ethereum (EIP-4361): the server builds the message, the wallet signs it. */
class AuthStore {
	session = $state<Session | null>(typeof window === 'undefined' ? null : load());
	signingIn = $state(false);

	/** A token that belongs to the connected wallet and has not expired. */
	token = $derived.by(() => {
		const s = this.session;
		const address = wallet.address;
		if (!s || !address) return null;
		if (s.address.toLowerCase() !== address.toLowerCase()) return null;
		if (Date.parse(s.expiresAt) <= Date.now()) return null;
		return s.token;
	});

	signedIn = $derived(this.token !== null);

	clear() {
		this.session = null;
		save(null);
	}

	/** Returns a valid token, asking the wallet to sign in when there is none. */
	async ensure(): Promise<string> {
		if (this.token) return this.token;
		const address = wallet.assertReady();
		const provider = wallet.provider;
		if (!provider) throw new AppError('no_wallet', 'No wallet found in this browser.');
		if (this.signingIn) throw new AppError('other', 'A sign-in is already waiting in your wallet.');
		this.signingIn = true;
		try {
			const { message } = await api.nonce(address);
			const client = createWalletClient({ account: address, transport: custom(provider) });
			const signature = await client.signMessage({ account: address, message });
			const res = await api.verify({ address, message, signature });
			this.session = { token: res.token, address: res.address, expiresAt: res.expiresAt };
			save(this.session);
			return res.token;
		} finally {
			this.signingIn = false;
		}
	}

	/** Runs an authenticated call; a 401 clears the token so the next attempt signs in again. */
	async call<T>(fn: (token: string) => Promise<T>): Promise<T> {
		const token = await this.ensure();
		try {
			return await fn(token);
		} catch (e) {
			if (e instanceof ApiError && e.status === 401) {
				this.clear();
				throw new AppError('auth', 'Your sign-in expired. Sign in again to continue.');
			}
			throw e;
		}
	}
}

export const auth = new AuthStore();
