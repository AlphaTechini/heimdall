import { app } from './app.svelte';
import { resolve } from '$app/paths';

export interface Link {
	href: string;
	external: boolean;
}

/** A public explorer when the server has one; the app's own /tx page on the local fork. */
export function txLink(hash: string): Link {
	const explorer = app.config?.explorer;
	if (explorer) return { href: explorer.txUrl + hash, external: true };
	return { href: resolve('/tx/[hash]', { hash }), external: false };
}

/** Address pages only exist on a public explorer. */
export function addressLink(address: string): Link | null {
	const explorer = app.config?.explorer;
	return explorer ? { href: explorer.addressUrl + address, external: true } : null;
}
