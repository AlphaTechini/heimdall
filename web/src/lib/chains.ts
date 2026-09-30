import { arbitrum, arbitrumSepolia } from 'viem/chains';

export interface ChainSpec {
	id: number;
	name: string;
	rpcUrl: string;
	explorerUrl?: string;
}

/** Local fork: anvil's default RPC. The name is fixed so the wallet shows something recognisable. */
export const LOCAL_CHAIN_ID = 31337;

export function chainSpec(id: number, serverName: string): ChainSpec {
	if (id === LOCAL_CHAIN_ID) {
		return { id, name: 'Heimdall local fork', rpcUrl: 'http://127.0.0.1:8545' };
	}
	if (id === arbitrum.id) {
		return {
			id,
			name: arbitrum.name,
			rpcUrl: arbitrum.rpcUrls.default.http[0],
			explorerUrl: arbitrum.blockExplorers.default.url
		};
	}
	if (id === arbitrumSepolia.id) {
		return {
			id,
			name: arbitrumSepolia.name,
			rpcUrl: arbitrumSepolia.rpcUrls.default.http[0],
			explorerUrl: arbitrumSepolia.blockExplorers.default.url
		};
	}
	return { id, name: serverName, rpcUrl: '' };
}

const KNOWN: Record<number, string> = {
	1: 'Ethereum',
	10: 'Optimism',
	137: 'Polygon',
	8453: 'Base',
	42161: 'Arbitrum One',
	421614: 'Arbitrum Sepolia',
	31337: 'a local test network'
};

export function chainLabel(id: number | null): string {
	if (id === null) return 'an unknown network';
	return KNOWN[id] ?? `network ${id}`;
}
