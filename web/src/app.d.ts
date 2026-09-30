// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
import type { Eip1193Provider } from '$lib/wallet.svelte';

declare global {
	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		// interface PageState {}
		// interface Platform {}
	}
	interface Window {
		ethereum?: Eip1193Provider;
	}
}

export {};
